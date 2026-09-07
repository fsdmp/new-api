//go:build linux

package exceltools

import (
	"context"
	"os/exec"
	"syscall"
	"time"

	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// NativeRunner isolates code execution with plain Linux namespaces driven by
// Go's exec support — no external sandbox binary. It mirrors NsjailRunner's
// guarantees: user/mount/pid/net/ipc/uts namespaces, chroot to the same
// prebuilt rootfs, tmpfs scratch on /tmp, nobody (65534) with zero capability
// sets after the drop, rlimits, wall-clock kill. Requires the gateway to run
// as root (to write the uid/gid maps) — bare-metal deployment satisfies this.
//
// Isolation design (live-verified on the production host, see nativeScript):
//   - uid/gid maps {0→0, 65534→65534}: the process enters as ns-root — the
//     ONLY way mounts work, because a process whose uid is unmapped in its own
//     userns cannot exercise its namespace capabilities (single-map {0→65534}
//     was tried and every mount silently failed);
//   - the trusted bootstrap script mounts tmpfs/proc, then drops to nobody
//     with setpriv: --no-new-privs permanently blocks suid/file-cap elevation
//     (rootfs suid-root binaries would otherwise grant REAL host-root euid,
//     since host uid 0 is mapped into the ns), and the cleared bounding/
//     inheritable/ambient sets strip every remaining capability;
//   - untrusted python never runs before the drop (it arrives on stdin).
type NativeRunner struct{}

func (r *NativeRunner) Run(ctx context.Context, userID int, code string) *RunOutput {
	_, rootfsPath := excel_tools.ResolveSandboxPaths()
	timeoutSec := excel_tools.GetSandboxConfig().TimeoutSec
	// 1s grace: native has no internal timer, so the ctx kill IS the wall
	// clock enforcer (SIGKILL on pid1 takes the whole pidns with it).
	return driveSandboxRun(ctx, userID, code, timeoutSec, time.Second, func(ctx context.Context) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "/bin/bash", "-c", nativeScript(timeoutSec))
		// Go applies SysProcAttr.Chroot FIRST and cmd.Dir AFTER, so Dir is
		// resolved inside the chroot: "/tmp" both silences bash's getcwd
		// complaint about the unreachable inherited cwd and lands the shell
		// on a valid directory. (A host path here fails exec with ENOENT.)
		cmd.Dir = "/tmp"
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: syscall.CLONE_NEWUSER | syscall.CLONE_NEWNS |
				syscall.CLONE_NEWPID | syscall.CLONE_NEWNET |
				syscall.CLONE_NEWIPC | syscall.CLONE_NEWUTS,
			UidMappings: []syscall.SysProcIDMap{
				{ContainerID: 0, HostID: 0, Size: 1},
				{ContainerID: 65534, HostID: 65534, Size: 1},
			},
			GidMappings: []syscall.SysProcIDMap{
				{ContainerID: 0, HostID: 0, Size: 1},
				{ContainerID: 65534, HostID: 65534, Size: 1},
			},
			Chroot:    rootfsPath,
			Pdeathsig: syscall.SIGKILL,
		}
		return cmd
	})
}
