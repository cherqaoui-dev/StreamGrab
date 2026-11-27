package settings

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"streamgrab/host/internal/atomicfile"
)

// mu keeps concurrent updates from overwriting each other's changes.
var mu sync.Mutex

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

// loadForUpdate returns an error when saved settings exist but can't be read.
// An unparseable file is set aside first, so the update doesn't destroy it.
func loadForUpdate(path string) (File, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return File{}, err
	}
	var file File
	if err := json.Unmarshal(raw, &file); err != nil {
		saved, moveErr := atomicfile.SetAside(path)
		if moveErr != nil {
			return File{}, errors.Join(err, moveErr)
		}
		log.Printf("unreadable settings moved to %s: %v", saved, err)
		return File{}, nil
	}
	return file, nil
}

func Update(path string, mutate func(*File)) error {
	if path == "" {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	file, err := loadForUpdate(path)
	if err != nil {
		return err
	}
	mutate(&file)
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, raw, 0o644)
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
