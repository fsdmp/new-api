//go:build !linux

package exceltools

import (
	"context"
)

// NativeRunner on non-linux platforms (dev machines): the namespace backend is
// linux-only. The type must exist on every platform so config parsing and the
// runner factory compile everywhere; Run always reports unavailable.
type NativeRunner struct{}

func (r *NativeRunner) Run(ctx context.Context, userID int, code string) *RunOutput {
	return &RunOutput{ExitCode: -1, Stderr: "native sandbox runner requires linux"}
}
