package canary

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

var reportIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

var ErrReportNotFound = errors.New("report not found")

type FileStore struct {
	dir string
}

func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, fmt.Errorf("report directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create report directory: %w", err)
	}
	return &FileStore{dir: dir}, nil
}

func (s *FileStore) Save(report AuditReport) error {
	if !reportIDPattern.MatchString(report.ID) {
		return fmt.Errorf("invalid report id")
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	encoded = append(encoded, '\n')
	temp, err := os.CreateTemp(s.dir, ".report-*")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return fmt.Errorf("secure temporary report: %w", err)
	}
	if _, err := temp.Write(encoded); err != nil {
		temp.Close()
		return fmt.Errorf("write report: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync report: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err := os.Rename(tempName, filepath.Join(s.dir, report.ID+".json")); err != nil {
		return fmt.Errorf("publish report: %w", err)
	}
	return nil
}

func (s *FileStore) Get(id string) (AuditReport, error) {
	if !reportIDPattern.MatchString(id) {
		return AuditReport{}, ErrReportNotFound
	}
	encoded, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return AuditReport{}, ErrReportNotFound
	}
	if err != nil {
		return AuditReport{}, fmt.Errorf("read report: %w", err)
	}
	var report AuditReport
	if err := json.Unmarshal(encoded, &report); err != nil {
		return AuditReport{}, fmt.Errorf("decode report: %w", err)
	}
	return report, nil
}
