// Package atomicfile saves small state files so that a crash can't leave them
// half written.
package atomicfile

import (
	"os"
	"path/filepath"
	"time"
)

// Write replaces path with data. The data goes to a temporary file in the same
// folder, is flushed to disk, and is then renamed over path.
func Write(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(name)
		}
	}()
	_, err = temp.Write(data)
	if err == nil {
		err = temp.Chmod(perm)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return rename(name, path)
}

// SetAside renames an unreadable file so it can be inspected or restored, and
// returns its new path.
func SetAside(path string) (string, error) {
	saved := path + ".corrupt-" + time.Now().UTC().Format("20060102-150405.000000000")
	if err := rename(path, saved); err != nil {
		return "", err
	}
	return saved, nil
}

// rename retries briefly because Windows refuses to replace a file while
// another program, such as a virus scanner, has it open.
func rename(from, to string) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 10 * time.Millisecond)
	}
	return err
}
