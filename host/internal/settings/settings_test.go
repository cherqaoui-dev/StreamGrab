package settings

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestUpdateKeepsUnreadableSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"downloadDir":`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, func(file *File) { file.CoreWarning = "x" }); err != nil {
		t.Fatal(err)
	}
	saved, err := filepath.Glob(path + ".corrupt-*")
	if err != nil || len(saved) != 1 {
		t.Fatalf("%v %v", saved, err)
	}
	raw, err := os.ReadFile(saved[0])
	if err != nil || string(raw) != `{"downloadDir":` {
		t.Fatalf("%q %v", raw, err)
	}
	if got := Load(path).CoreWarning; got != "x" {
		t.Fatalf("warning = %q", got)
	}
}

func TestConcurrentUpdatesKeepBothFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = Update(path, func(file *File) { file.DownloadDir = "dir" })
		}()
		go func() {
			defer wg.Done()
			_ = Update(path, func(file *File) { file.LastCoreCheck = time.Unix(1, 0) })
		}()
	}
	wg.Wait()
	got := Load(path)
	if got.DownloadDir != "dir" || got.LastCoreCheck.IsZero() {
		t.Fatalf("%+v", got)
	}
}
