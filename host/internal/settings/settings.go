package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type File struct {
	DownloadDir   string    `json:"downloadDir,omitempty"`
	AutoUpdate    *bool     `json:"autoUpdate,omitempty"`
	LastCoreCheck time.Time `json:"lastCoreCheck,omitempty"`
	CoreWarning   string    `json:"coreWarning,omitempty"`
}

func Load(path string) File {
	raw, err := os.ReadFile(path)
	if err != nil {
		return File{}
	}
	var file File
	if json.Unmarshal(raw, &file) != nil {
		return File{}
	}
	return file
}

func Update(path string, mutate func(*File)) error {
	if path == "" {
		return nil
	}
	file := Load(path)
	mutate(&file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

func PathBeside(store string) string {
	if store == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(store), "settings.json")
}

func Enabled(file File) bool {
	return file.AutoUpdate == nil || *file.AutoUpdate
}
