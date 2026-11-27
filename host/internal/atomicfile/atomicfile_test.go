package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")
	for _, text := range []string{"first", "second"} {
		if err := Write(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "second" {
		t.Fatalf("%q %v", raw, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %v", entries, err)
	}
}

func TestSetAsideKeepsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved, err := SetAside(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(saved)
	if err != nil || string(raw) != "broken" {
		t.Fatalf("%q %v", raw, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("original is still there")
	}
}
