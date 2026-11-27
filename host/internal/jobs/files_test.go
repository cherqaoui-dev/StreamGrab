package jobs

import (
	"context"
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
	sweepFragments(dir, map[string]bool{"abcdefghijk": true}, 24*time.Hour, time.Now())
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

func TestStartupSweepOnlyRemovesFinishedJobFiles(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(t.TempDir(), "jobs.json")
	saved := `[
		{"id":"1","videoId":"finished000","state":"failed"},
		{"id":"2","videoId":"paused00000","state":"paused"},
		{"id":"3","videoId":"mixed000000","state":"done"},
		{"id":"4","videoId":"mixed000000","state":"queued"}
	]`
	if err := os.WriteFile(store, []byte(saved), 0o644); err != nil {
		t.Fatal(err)
	}
	removed := "Clip [finished000].f137.mp4"
	kept := []string{
		"ubuntu.iso.part",
		"Other [zzzzzzzzzzz].f137.mp4",
		"finished000.part",
		"Clip [finished000].mp4",
		"Clip [paused00000].mp4.part",
		"Clip [mixed000000].f251.webm",
	}
	past := time.Now().Add(-48 * time.Hour)
	for _, name := range append([]string{removed}, kept...) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, past, past); err != nil {
			t.Fatal(err)
		}
	}
	New(context.Background(), &fakeRunner{}, &fakeSuspend{}, nil, dir, store)
	if _, err := os.Stat(filepath.Join(dir, removed)); !os.IsNotExist(err) {
		t.Fatal("stale fragment of a failed job was kept")
	}
	for _, name := range kept {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s was removed", name)
		}
	}
}

func TestUnreadableQueueIsSetAside(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	broken := `[{"id":"a",`
	if err := os.WriteFile(store, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	New(context.Background(), &fakeRunner{}, &fakeSuspend{}, nil, t.TempDir(), store)
	saved, err := filepath.Glob(store + ".corrupt-*")
	if err != nil || len(saved) != 1 {
		t.Fatalf("%v %v", saved, err)
	}
	raw, err := os.ReadFile(saved[0])
	if err != nil || string(raw) != broken {
		t.Fatalf("%q %v", raw, err)
	}
}
