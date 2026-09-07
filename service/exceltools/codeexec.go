package exceltools

import (
	"context"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// CodeExecutionExecutor executes the virtual code_execution tool in the
// nsjail sandbox.
type CodeExecutionExecutor struct{}

func init() {
	RegisterExecutor(&CodeExecutionExecutor{})
}

func (e *CodeExecutionExecutor) Name() string { return excel_tools.ToolCodeExecution }

func (e *CodeExecutionExecutor) Execute(sess *Session, call *CapturedCall) *ExecResult {
	code, ok := call.Param("code")
	if !ok || strings.TrimSpace(code) == "" {
		return invalidInputResult("code_execution 需要 string 参数 code（要执行的 Python 代码）")
	}
	if !excel_tools.SandboxAvailable() {
		return &ExecResult{
			ModelText:   "代码执行沙箱未启用或未部署，请直接基于已有信息回答，并说明无法执行代码。",
			Err:         &ToolError{Code: "sandbox_unavailable", Message: "sandbox not available"},
			StatusShort: "sandbox_unavailable",
		}
	}

	run := NewNsjailRunner().Run(context.Background(), sess.UserID, code)
	if run.DurationMs > 3000 || run.TimedOut || run.ExitCode != 0 {
		common.SysLog("[excel-tools] code_execution done: exit=" +
			strconv.Itoa(run.ExitCode) + " timedOut=" + strconv.FormatBool(run.TimedOut) +
			" durMs=" + strconv.FormatInt(run.DurationMs, 10))
	}
	return execCodeResult(run)
}
