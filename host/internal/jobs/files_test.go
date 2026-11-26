package jobs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepKeepsFreshFragments(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "Clip [abcdefghijk].f137.mp4")
	fresh := filepath.Join(dir, "Clip [abcdefghijk].part")
	final := filepath.Join(dir, "Clip [abcdefghijk].mp4")
	for _, name := range []string{old, fresh, final} {
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	sweepFragments(dir, 24*time.Hour, time.Now())
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old fragment was kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); err != nil {
		t.Fatal(err)
	}
}
