package exceltools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/excel_tools"
)

// The M2 file service. Bodies live on local disk (bare-metal host, no object
// storage — docs/excel-files-m2-design.md §2), relative to the gateway
// working directory, same convention as the sandbox rootfs:
//
//	files/<uid>/<file_id>                                        body
//	sandbox/rootfs/files/input/<file_id>/<filename>              sandbox read-only link
//
// The sandbox link is a hard link (same filesystem, zero copy; falls back to
// a copy cross-device). rootfs dirs are root-owned 0755 so nobody inside the
// jail cannot write there — read-only semantics come for free.

const (
	filesRoot         = "files"
	sandboxInputDir   = "files/input" // relative to rootfs
	fileIDPrefix      = "file-"
	fileIDRandomBytes = 12            // "file-" + 24 hex
	maxFilenameBytes  = 200
)

// validFileIDRe guards every id that reaches a filesystem path. Ids are
// gateway-generated but user-supplied values must still be validated before
// they are joined into paths (path traversal defense).
var validFileIDRe = regexp.MustCompile(`^file-[0-9a-f]{24}$`)

// ValidExcelFileID reports whether id is well-formed.
func ValidExcelFileID(id string) bool { return validFileIDRe.MatchString(id) }

// NewExcelFileID generates "file-" + 24 hex chars.
func NewExcelFileID() (string, error) {
	buf := make([]byte, fileIDRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fileIDPrefix + hex.EncodeToString(buf), nil
}

// SanitizeFilename strips directories and control characters and caps the
// length. The sanitized value is what gets stored and re-used for every later
// path join, so a hostile original name never touches the filesystem twice.
func SanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		return "upload.bin"
	}
	// Cap by bytes without splitting a rune.
	if len(out) > maxFilenameBytes {
		runes := []rune(out)
		for len(string(runes)) > maxFilenameBytes && len(runes) > 0 {
			runes = runes[:len(runes)-1]
		}
		out = string(runes)
	}
	return out
}

// extensionAllowed checks the lowercase extension against the whitelist.
func extensionAllowed(filename string) bool {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if ext == "" {
		return false
	}
	for _, allowed := range excel_tools.GetFilesConfig().Extensions {
		if ext == strings.ToLower(allowed) {
			return true
		}
	}
	return false
}

// mimeFor guesses the stored mime type: extension map first (office types
// have no sniffable magic), then http.DetectContentType.
func mimeFor(filename string, head []byte) string {
	if m := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename))); m != "" {
		return m
	}
	if len(head) > 0 {
		return http.DetectContentType(head)
	}
	return "application/octet-stream"
}

// excelFileBodyPath returns the body path for a file (absolute-ized the same
// way as the sandbox paths: relative to the gateway working directory).
func excelFileBodyPath(userId int, fileID string) string {
	return filepath.Join(filesRoot, fmt.Sprintf("%d", userId), fileID)
}

// SaveExcelFile stores an uploaded body and its metadata row after all quota
// checks pass. head is the first bytes of the body (already read for mime
// detection by the caller or the multipart reader).
func SaveExcelFile(userId int, filename, purpose string, size int64, r io.Reader) (*model.ExcelFile, error) {
	cfg := excel_tools.GetFilesConfig()
	if !cfg.Enabled {
		return nil, fmt.Errorf("file service is disabled")
	}
	filename = SanitizeFilename(filename)
	if !extensionAllowed(filename) {
		return nil, fmt.Errorf("file type not allowed: %s (allowed: %s)",
			strings.TrimPrefix(filepath.Ext(filename), "."), strings.Join(cfg.Extensions, ","))
	}
	if size <= 0 || size > int64(cfg.MaxSizeMB)<<20 {
		return nil, fmt.Errorf("file size must be > 0 and <= %dMB", cfg.MaxSizeMB)
	}

	cnt, total, err := model.ExcelFileUsage(userId)
	if err != nil {
		return nil, err
	}
	if cnt >= int64(cfg.MaxFilesPerUser) {
		return nil, fmt.Errorf("file count limit reached (%d files)", cfg.MaxFilesPerUser)
	}
	if total+size > int64(cfg.MaxMBPerUser)<<20 {
		return nil, fmt.Errorf("storage limit reached (%dMB)", cfg.MaxMBPerUser)
	}

	fileID, err := NewExcelFileID()
	if err != nil {
		return nil, err
	}
	bodyPath := excelFileBodyPath(userId, fileID)
	if err := os.MkdirAll(filepath.Dir(bodyPath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(bodyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	written, copyErr := io.Copy(f, io.LimitReader(r, size))
	if syncErr := f.Sync(); syncErr != nil && copyErr == nil {
		copyErr = syncErr
	}
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(bodyPath)
		return nil, copyErr
	}
	if written != size {
		_ = os.Remove(bodyPath)
		return nil, fmt.Errorf("short write: %d/%d bytes", written, size)
	}

	// Read the head back for mime detection (cheap, file is local and small).
	head := make([]byte, 512)
	if hf, err := os.Open(bodyPath); err == nil {
		n, _ := hf.Read(head)
		head = head[:n]
		_ = hf.Close()
	}

	now := time.Now().Unix()
	row := &model.ExcelFile{
		Id:        fileID,
		UserId:    userId,
		Filename:  filename,
		Mime:      mimeFor(filename, head),
		SizeBytes: size,
		Purpose:   SanitizePurpose(purpose),
		CreatedAt: now,
		ExpiresAt: now + int64(cfg.TTLDays)*86400,
	}
	if err := model.CreateExcelFile(row); err != nil {
		_ = os.Remove(bodyPath)
		return nil, err
	}
	return row, nil
}

// SanitizePurpose caps and cleans the optional purpose field.
func SanitizePurpose(p string) string {
	p = strings.TrimSpace(p)
	if len(p) > 64 {
		p = p[:64]
	}
	if p == "" {
		p = "default"
	}
	return p
}

// OpenExcelFile validates ownership and opens the body for streaming.
func OpenExcelFile(id string, userId int) (*model.ExcelFile, *os.File, error) {
	if !ValidExcelFileID(id) {
		return nil, nil, model.ErrExcelFileNotFound
	}
	row, err := model.GetExcelFile(id, userId)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(excelFileBodyPath(userId, row.Id))
	if err != nil {
		return nil, nil, model.ErrExcelFileNotFound
	}
	return row, f, nil
}

// DeleteExcelFile removes the row, the body and any sandbox link dir.
func DeleteExcelFile(id string, userId int) error {
	if !ValidExcelFileID(id) {
		return model.ErrExcelFileNotFound
	}
	row, err := model.GetExcelFile(id, userId)
	if err != nil {
		return err
	}
	if err := model.DeleteExcelFile(id, userId); err != nil {
		return err
	}
	_ = os.Remove(excelFileBodyPath(userId, row.Id))
	_ = os.RemoveAll(sandboxLinkDir(row.Id))
	// Best-effort prune of an empty per-user dir.
	_ = os.Remove(filepath.Dir(excelFileBodyPath(userId, row.Id)))
	return nil
}

// sandboxLinkDir is the per-file directory inside the sandbox rootfs where
// the model-facing copies are hard-linked.
func sandboxLinkDir(fileID string) string {
	_, rootfs := excel_tools.ResolveSandboxPaths()
	return filepath.Join(rootfs, sandboxInputDir, fileID)
}

// LinkExcelFileIntoSandbox makes a file visible at
// /files/input/<file_id>/<filename> inside the jail (read-only in effect).
// Idempotent; ownership is validated before anything touches disk.
func LinkExcelFileIntoSandbox(id string, userId int) (*model.ExcelFile, error) {
	if !ValidExcelFileID(id) {
		return nil, model.ErrExcelFileNotFound
	}
	row, err := model.GetExcelFile(id, userId)
	if err != nil {
		return nil, err
	}
	body := excelFileBodyPath(userId, row.Id)
	if _, err := os.Stat(body); err != nil {
		return nil, model.ErrExcelFileNotFound
	}
	linkDir := sandboxLinkDir(row.Id)
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		return nil, err
	}
	dst := filepath.Join(linkDir, row.Filename)
	if err := os.Link(body, dst); err != nil {
		if os.IsExist(err) {
			return row, nil // already linked (idempotent)
		}
		// Cross-device (EXDEV) or unsupported: fall back to a copy.
		in, e1 := os.Open(body)
		if e1 != nil {
			return nil, e1
		}
		defer in.Close()
		tmp := dst + ".tmp"
		out, e2 := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if e2 != nil {
			return nil, e2
		}
		_, e3 := io.Copy(out, in)
		if e4 := out.Close(); e3 == nil {
			e3 = e4
		}
		if e3 != nil {
			_ = os.Remove(tmp)
			return nil, e3
		}
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			return nil, err
		}
	}
	return row, nil
}

// CleanExpiredExcelFiles drops expired rows plus their bodies and sandbox
// links. Called by the background sweeper; safe to run concurrently with
// everything else (all steps are idempotent).
func CleanExpiredExcelFiles() (int, error) {
	files, err := model.ExpiredExcelFileIds(500)
	if err != nil || len(files) == 0 {
		return 0, err
	}
	n := 0
	for _, f := range files {
		if err := model.DeleteExcelFile(f.Id, f.UserId); err == nil {
			_ = os.Remove(excelFileBodyPath(f.UserId, f.Id))
			_ = os.RemoveAll(sandboxLinkDir(f.Id))
			n++
		}
	}
	if n > 0 {
		common.SysLog(fmt.Sprintf("[excel-files] TTL sweep removed %d expired files", n))
	}
	return n, nil
}

// StartExcelFileSweeper runs the TTL sweep every 6 hours (plus once shortly
// after boot). Bare-metal host has no cron; the gateway owns its lifecycle.
func StartExcelFileSweeper() {
	go func() {
		time.Sleep(2 * time.Minute) // let boot settle
		for {
			if _, err := CleanExpiredExcelFiles(); err != nil {
				common.SysError("[excel-files] TTL sweep failed: " + err.Error())
			}
			time.Sleep(6 * time.Hour)
		}
	}()
}

// DocumentBlockHint is injected in place of a document content block so the
// model knows how to reach the file from code_execution.
func DocumentBlockHint(row *model.ExcelFile) string {
	return fmt.Sprintf(`[用户上传了文件「%s」（%s，%.1fKB）。
文件已就绪：在 code_execution 工具调用中传入 file_ids:["%s"]，随后即可在沙箱中以只读路径 /files/input/%s/%s 访问该文件。
解析建议：PDF 用 pdfplumber；xlsx/xls/csv 用 pandas 或 openpyxl；docx 用 python-docx。]`,
		row.Filename, row.Mime, float64(row.SizeBytes)/1024, row.Id, row.Id, row.Filename)
}

// DocumentBlockDisabledHint replaces document blocks when the service is off.
const DocumentBlockDisabledHint = "[用户发送了一个文档附件，但当前环境未启用文件服务，无法读取其内容。请提示用户直接粘贴关键内容。]"

// DocumentBlockInlineHint replaces inline base64 documents (not stored by
// this service; the client should use the upload entry point instead).
const DocumentBlockInlineHint = "[用户在消息内嵌了一个文档（base64 直传）。当前环境不支持内嵌文档，请提示用户通过上传入口使用文件服务。]"

// DocumentBlockNotFoundHint replaces document blocks whose file reference
// does not exist (or belongs to someone else — indistinguishable on purpose).
const DocumentBlockNotFoundHint = "[用户引用了一个文档附件，但文件不存在或已过期。请提示用户重新上传。]"

// DocumentHintForBlock inspects an Anthropic document content block and
// returns the text hint to substitute for it. userId scopes the ownership
// check so one user can never confirm the existence of another's file.
func DocumentHintForBlock(blockMap map[string]interface{}, userId int) string {
	if !excel_tools.FilesEnabled() {
		return DocumentBlockDisabledHint
	}
	src, _ := blockMap["source"].(map[string]interface{})
	if src == nil {
		return DocumentBlockNotFoundHint
	}
	if t, _ := src["type"].(string); t == "base64" {
		return DocumentBlockInlineHint
	}
	fileID, _ := src["file_id"].(string)
	if !ValidExcelFileID(fileID) {
		return DocumentBlockNotFoundHint
	}
	row, err := model.GetExcelFile(fileID, userId)
	if err != nil {
		return DocumentBlockNotFoundHint
	}
	return DocumentBlockHint(row)
}
