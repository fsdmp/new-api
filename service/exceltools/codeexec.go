package exceltools

import (
	"context"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// CodeExecutionExecutor executes the virtual code_execution tool in the
// configured sandbox runner, with optional /files/input linkage (M2).
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

	// M2: stage referenced uploads into the jail before running. Failures are
	// reported in-band (the model retries without the file) — a bad file id
	// must not kill an otherwise fine computation.
	prepend := stageFilesForRun(sess.UserID, call)

	run := NewCodeRunner().Run(context.Background(), sess.UserID, code)
	if run.DurationMs > 3000 || run.TimedOut || run.ExitCode != 0 {
		common.SysLog("[excel-tools] code_execution done: exit=" +
			strconv.Itoa(run.ExitCode) + " timedOut=" + strconv.FormatBool(run.TimedOut) +
			" durMs=" + strconv.FormatInt(run.DurationMs, 10))
	}
	res := execCodeResult(run)
	if prepend != "" {
		res.ModelText = prepend + res.ModelText
		if blocks, ok := res.Block.([]interface{}); ok && len(blocks) == 1 {
			if block, ok := blocks[0].(map[string]interface{}); ok {
				if content, ok := block["content"].([]interface{}); ok && len(content) == 1 {
					if text, ok := content[0].(map[string]interface{}); ok {
						if s, ok := text["text"].(string); ok {
							text["text"] = prepend + s
						}
					}
				}
			}
		}
	}
	return res
}

// stageFilesForRun links the code_execution file_ids into the sandbox rootfs
// and returns an empty string on full success, or a short notice to prepend
// to the run output when something could not be staged.
func stageFilesForRun(userID int, call *CapturedCall) string {
	if !excel_tools.FilesEnabled() {
		return ""
	}
	ids := call.ParamStringSlice("file_ids")
	if len(ids) == 0 {
		return ""
	}
	limit := excel_tools.GetFilesConfig().MaxFileIdsPerRun
	if len(ids) > limit {
		ids = ids[:limit]
	}
	var problems []string
	for _, id := range ids {
		row, err := LinkExcelFileIntoSandbox(id, userID)
		if err != nil {
			problems = append(problems, id)
			continue
		}
		common.SysLog("[excel-tools] file staged for sandbox: " + id + " -> /files/input/" + row.Id + "/" + row.Filename)
	}
	if len(problems) == 0 {
		return ""
	}
	return "注意：file_ids 中 " + strings.Join(problems, ", ") +
		" 不存在、已过期或不属于当前用户，未加载；其余文件已就位。\n"
}
