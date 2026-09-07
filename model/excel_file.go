package model

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ExcelFile is the metadata row behind /excel/v1/files (M2 file service).
// Bodies live on local disk at <data>/files/<uid>/<id>; ids are gateway
// generated ("file-" + 24 hex) so all three databases behave identically
// (no auto-increment reliance, repo rule 2).
type ExcelFile struct {
	Id        string `json:"id" gorm:"primaryKey;type:varchar(40)"`
	UserId    int    `json:"user_id" gorm:"index"`
	Filename  string `json:"filename" gorm:"type:varchar(255)"`
	Mime      string `json:"mime" gorm:"type:varchar(128)"`
	SizeBytes int64  `json:"size_bytes"`
	Purpose   string `json:"purpose" gorm:"type:varchar(64)"`
	CreatedAt int64  `json:"created_at" gorm:"index"` // unix seconds
	ExpiresAt int64  `json:"expires_at" gorm:"index"` // unix seconds
}

func (ExcelFile) TableName() string { return "excel_files" }

var ErrExcelFileNotFound = errors.New("excel file not found")

// CreateExcelFile inserts a metadata row.
func CreateExcelFile(f *ExcelFile) error {
	if DB == nil {
		return nil
	}
	return DB.Create(f).Error
}

// GetExcelFile returns one row scoped to the owner — the single lookup used
// by download/delete/sandbox-link so cross-user access can never succeed.
// A nil DB (unit tests before InitDB) reads as not-found, never panics.
func GetExcelFile(id string, userId int) (*ExcelFile, error) {
	if id == "" || DB == nil {
		return nil, ErrExcelFileNotFound
	}
	var f ExcelFile
	err := DB.Where("id = ? AND user_id = ?", id, userId).First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrExcelFileNotFound
		}
		return nil, err
	}
	return &f, nil
}

// ListExcelFiles returns a user's files newest-first, capped at 100.
func ListExcelFiles(userId int) ([]*ExcelFile, error) {
	if DB == nil {
		return nil, nil
	}
	var files []*ExcelFile
	err := DB.Where("user_id = ?", userId).
		Order("created_at desc").Limit(100).Find(&files).Error
	return files, err
}

// ExcelFileUsage returns (count, total bytes) for a user's quota check.
func ExcelFileUsage(userId int) (int64, int64, error) {
	if DB == nil {
		return 0, 0, nil
	}
	var res struct {
		Cnt  int64
		Size int64
	}
	err := DB.Model(&ExcelFile{}).Where("user_id = ?", userId).
		Select("COUNT(*) AS cnt, COALESCE(SUM(size_bytes), 0) AS size").
		Scan(&res).Error
	return res.Cnt, res.Size, err
}

// DeleteExcelFile removes the metadata row (hard delete — no audit need).
func DeleteExcelFile(id string, userId int) error {
	if DB == nil {
		return nil
	}
	return DB.Where("id = ? AND user_id = ?", id, userId).Delete(&ExcelFile{}).Error
}

// ExpiredExcelFileIds returns ids of rows past their TTL in batches, plus
// their owners so the cleanup task can drop disk bodies under files/<uid>/.
func ExpiredExcelFileIds(batch int) ([]*ExcelFile, error) {
	if DB == nil {
		return nil, nil
	}
	if batch <= 0 {
		batch = 500
	}
	var files []*ExcelFile
	err := DB.Where("expires_at > 0 AND expires_at < ?", time.Now().Unix()).
		Limit(batch).Find(&files).Error
	return files, err
}
