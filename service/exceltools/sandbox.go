package exceltools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// RunOutput is the outcome of one sandboxed execution.
type RunOutput struct {
	Stdout      string
	Stderr      string
	ExitCode    int
	TimedOut    bool
	DurationMs  int64
	OutputTrunc bool // stdout/stderr hit the capture cap
}

// CodeRunner abstracts the code sandbox backend (NsjailRunner today; an E2B-
// style SaaS backend would need a data-compliance review first — design §12).
type CodeRunner interface {
	Run(ctx context.Context, userID int, code string) *RunOutput
}

// sandboxOutputCap bounds captured stdout/stderr (each) so a runaway script
// can't balloon gateway memory.
const sandboxOutputCap = 64 * 1024

// NsjailRunner executes Python in an nsjail one-shot jail: chroot to the
// prebuilt rootfs, nobody/nogroup, no network namespace interfaces, rlimits,
// wall-clock kill. The process IS the sandbox — no cleanup, no daemon.
type NsjailRunner struct{}

// pythonInRootfs is the interpreter path inside the prebuilt rootfs
// (python:3.11-slim layout; deploy/excel-sandbox/Dockerfile symlinks
// /usr/bin/python3 as well). `-I` = isolated mode: no user site, no env vars.
const pythonInRootfs = "/usr/local/bin/python3"

// The jail needs a writable scratch dir: tmpfs on /tmp, everything else ro.
var nsjailFixedArgs = []string{
	"-Mo",
	"--user", "65534", "--group", "65534",
	"--cwd", "/tmp",
	"--tmpfsmount", "/tmp:size=32m",
	"--rlimit_as", "1024",
	"--rlimit_nproc", "64",
	"--rlimit_fsize", "32",
	"--", pythonInRootfs, "-I", "-",
}

// Global concurrency semaphore + per-user in-flight gate (risk table:
// per-user 并发 1 + 全局信号量). Semaphore size is fixed at first use;
// changing sandbox.max_concurrent takes effect after restart.
var (
	sandboxSemOnce  sync.Once
	sandboxSem      chan struct{}
	sandboxUserMu   sync.Mutex
	sandboxInflight = map[int]int{}
)

func acquireSandboxSlot(userID int) (release func(), ok bool) {
	sandboxSemOnce.Do(func() {
		n := excel_tools.GetSandboxConfig().MaxConcurrent
		if n <= 0 {
			n = 4
		}
		sandboxSem = make(chan struct{}, n)
	})
	select {
	case sandboxSem <- struct{}{}:
	default:
		return nil, false // global limit reached — fail fast, don't queue
	}
	sandboxUserMu.Lock()
	if sandboxInflight[userID] >= 1 {
		sandboxUserMu.Unlock()
		<-sandboxSem
		return nil, false
	}
	sandboxInflight[userID]++
	sandboxUserMu.Unlock()
	return func() {
		sandboxUserMu.Lock()
		sandboxInflight[userID]--
		sandboxUserMu.Unlock()
		<-sandboxSem
	}, true
}

func (r *NsjailRunner) Run(ctx context.Context, userID int, code string) *RunOutput {
	start := time.Now()
	cfg := excel_tools.GetSandboxConfig()
	if !excel_tools.SandboxAvailable() {
		return &RunOutput{ExitCode: -1, Stderr: "sandbox not available", DurationMs: time.Since(start).Milliseconds()}
	}

	release, ok := acquireSandboxSlot(userID)
	if !ok {
		return &RunOutput{ExitCode: -1, Stderr: "sandbox busy (concurrency limit)", DurationMs: time.Since(start).Milliseconds()}
	}
	defer release()

	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	// +3s grace so nsjail's own --time_limit fires first and its log is captured.
	ctx, cancel := context.WithTimeout(ctx, timeout+3*time.Second)
	defer cancel()

	args := make([]string, 0, len(nsjailFixedArgs)+4)
	args = append(args, "-Mo", "--chroot", cfg.RootfsPath,
		"--time_limit", fmt.Sprintf("%d", cfg.TimeoutSec),
		"--rlimit_cpu", fmt.Sprintf("%d", cfg.TimeoutSec))
	args = append(args, nsjailFixedArgs...)

	cmd := exec.CommandContext(ctx, cfg.NsjailPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return &RunOutput{ExitCode: -1, Stderr: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	stdoutW := &limitedBuffer{cap: sandboxOutputCap}
	stderrW := &limitedBuffer{cap: sandboxOutputCap}
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		return &RunOutput{ExitCode: -1, Stderr: err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	if _, err := io.WriteString(stdin, code); err != nil {
		_ = stdin.Close()
		_ = cmd.Wait()
		return &RunOutput{ExitCode: -1, Stderr: "write stdin: " + err.Error(), DurationMs: time.Since(start).Milliseconds()}
	}
	_ = stdin.Close()

	waitErr := cmd.Wait()
	out := &RunOutput{
		Stdout:      stdoutW.String(),
		Stderr:      stderrW.String(),
		OutputTrunc: stdoutW.truncated || stderrW.truncated,
		DurationMs:  time.Since(start).Milliseconds(),
	}
	if cmd.ProcessState != nil {
		out.ExitCode = cmd.ProcessState.ExitCode()
	}
	if ctx.Err() == context.DeadlineExceeded || strings.Contains(strings.ToLower(out.Stderr), "timed out") {
		out.TimedOut = true
	}
	if waitErr != nil && out.ExitCode == 0 {
		out.ExitCode = -1
	}
	return out
}

// limitedBuffer caps captured bytes; further writes are counted, not stored.
type limitedBuffer struct {
	buf        bytes.Buffer
	cap        int
	truncated  bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.cap {
		keep := b.cap - b.buf.Len()
		if keep > 0 {
			b.buf.Write(p[:keep])
		}
		b.truncated = true
		return len(p), nil // pretend success so the pipe never blocks
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) String() string { return b.buf.String() }

// NewNsjailRunner returns the default runner (kept behind a constructor for
// future config-driven backend selection).
func NewNsjailRunner() CodeRunner { return &NsjailRunner{} }

// execCodeResult builds the ExecResult for a sandbox run.
func execCodeResult(run *RunOutput) *ExecResult {
	var b strings.Builder
	if run.TimedOut {
		b.WriteString("代码执行超时（超过时间限制被强制终止），请优化算法或减少计算量。\n")
	} else if run.ExitCode != 0 {
		fmt.Fprintf(&b, "代码执行失败（退出码 %d）。\n", run.ExitCode)
	}
	if run.Stdout != "" {
		b.WriteString(run.Stdout)
		if !strings.HasSuffix(run.Stdout, "\n") {
			b.WriteString("\n")
		}
	}
	if run.Stderr != "" {
		b.WriteString("stderr:\n" + run.Stderr)
	}
	if run.OutputTrunc {
		b.WriteString("\n（输出过长已截断）")
	}
	modelText := b.String()
	if len([]rune(modelText)) > 32*1024 {
		modelText = string([]rune(modelText)[:32*1024]) + "\n…（输出过长已截断）"
	}

	status := "ok"
	if run.TimedOut {
		status = "timeout"
	} else if run.ExitCode != 0 {
		status = "nonzero_exit"
	}

	block := map[string]interface{}{
		"type": "code_execution_result",
		"content": []interface{}{
			map[string]interface{}{"type": "text", "text": modelText},
		},
	}
	return &ExecResult{
		Block:       []interface{}{block},
		ModelText:   modelText,
		StatusShort: status,
		Err:         nil,
	}
}
