package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"streamgrab/host/internal/media"
)

func newTestManager(t *testing.T, runner Runner) *Manager {
	t.Helper()
	manager := New(context.Background(), runner, &fakeSuspend{}, nil, t.TempDir(), "")
	t.Cleanup(manager.Shutdown)
	return manager
}

func enqueue(t *testing.T, m *Manager, video string) string {
	t.Helper()
	result, err := m.Enqueue(Request{VideoID: video, Height: 720, VideoFormatID: "136"})
	if err != nil {
		t.Fatal(err)
	}
	return result.Job.ID
}

func stateOf(m *Manager, id string) string {
	for _, job := range m.List() {
		if job.ID == id {
			return job.State
		}
	}
	return ""
}

func currentHandle(r *fakeRunner, starts int, t *testing.T) *fakeHandle {
	t.Helper()
	var handle *fakeHandle
	waitUntil(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		handle = r.handle
		return r.starts == starts
	})
	return handle
}

func TestPausingAWaitingJobDoesNotBlockTheQueue(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestManager(t, runner)
	enqueue(t, m, "aaaaaaaaaaa")
	second := enqueue(t, m, "bbbbbbbbbbb")
	third := enqueue(t, m, "ccccccccccc")
	handle := currentHandle(runner, 1, t)
	if err := m.Pause(second); err != nil {
		t.Fatal(err)
	}
	handle.done <- nil
	waitUntil(t, func() bool { return stateOf(m, third) == StateDownloadingVideo })
	if got := stateOf(m, second); got != StatePaused {
		t.Fatalf("second = %s", got)
	}
}

func TestCancelThenRemove(t *testing.T) {
	runner := &fakeRunner{}
	m := newTestManager(t, runner)
	first := enqueue(t, m, "aaaaaaaaaaa")
	second := enqueue(t, m, "bbbbbbbbbbb")
	currentHandle(runner, 1, t)
	if err := m.Cancel(first); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return stateOf(m, second) == StateDownloadingVideo })
	if got := stateOf(m, first); got != StateCancelled {
		t.Fatalf("first = %s", got)
	}
	if err := m.Cancel(first); err != nil {
		t.Fatal(err)
	}
	if len(m.List()) != 1 {
		t.Fatalf("%+v", m.List())
	}
}

// writingRunner reports a file for each job, as yt-dlp does.
type writingRunner struct{ fakeRunner }

func (r *writingRunner) Start(ctx context.Context, job Job, onProgress func(media.Progress)) (Handle, error) {
	path := filepath.Join(job.Dir, "T ["+job.VideoID+"].f136.mp4")
	if err := os.WriteFile(path+".part", []byte("x"), 0o644); err != nil {
		return nil, err
	}
	onProgress(media.Progress{Stage: StateDownloadingVideo, File: path})
	return r.fakeRunner.Start(ctx, job, onProgress)
}

func TestCancelRemovesOnlyTheJobsFiles(t *testing.T) {
	runner := &writingRunner{}
	m := newTestManager(t, runner)
	other := filepath.Join(m.DownloadDir(), "Mine [aaaaaaaaaaa].zip.part")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := enqueue(t, m, "aaaaaaaaaaa")
	currentHandle(&runner.fakeRunner, 1, t)
	if err := m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(m.DownloadDir(), "T [aaaaaaaaaaa].f136.mp4.part")
	waitUntil(t, func() bool {
		_, err := os.Stat(own)
		return os.IsNotExist(err)
	})
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another program's file was removed")
	}
}

func TestResumeAfterStoppingStartsOneProcessAtATime(t *testing.T) {
	runner := &fakeRunner{}
	m := New(context.Background(), runner, &fakeSuspend{fail: true}, nil, t.TempDir(), "")
	t.Cleanup(m.Shutdown)
	id := enqueue(t, m, "aaaaaaaaaaa")
	currentHandle(runner, 1, t)
	if err := m.Pause(id); err != nil {
		t.Fatal(err)
	}
	if err := m.Resume(id); err != nil {
		t.Fatal(err)
	}
	currentHandle(runner, 2, t)
	time.Sleep(20 * time.Millisecond)
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.starts != 2 {
		t.Fatalf("starts = %d", runner.starts)
	}
}

func TestRetryingJobKeepsItsPlace(t *testing.T) {
	runner := &failRunner{err: errors.New("connection reset")}
	m := newTestManager(t, runner)
	m.backoff = func(int) time.Duration { return time.Hour }
	first := enqueue(t, m, "aaaaaaaaaaa")
	waitUntil(t, func() bool { return stateOf(m, first) == StateQueued })
	second := enqueue(t, m, "bbbbbbbbbbb")
	if got := stateOf(m, second); got != StateQueued {
		t.Fatalf("second = %s", got)
	}
}

func TestShutdownLeavesJobResumable(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(t.TempDir(), "jobs.json")
	runner := &fakeRunner{}
	m := New(context.Background(), runner, &fakeSuspend{}, nil, dir, store)
	enqueue(t, m, "aaaaaaaaaaa")
	currentHandle(runner, 1, t)
	m.Shutdown()
	reloaded := New(context.Background(), &fakeRunner{}, &fakeSuspend{}, nil, dir, store)
	if got := reloaded.List()[0].State; got != StatePaused {
		t.Fatalf("state = %s", got)
	}
}

func TestHistoryIsBounded(t *testing.T) {
	store := filepath.Join(t.TempDir(), "jobs.json")
	text := "["
	for i := 0; i < maxFinished+5; i++ {
		text += fmt.Sprintf(`{"id":"%d","state":"done"},`, i)
	}
	text += `{"id":"waiting","state":"paused"}]`
	if err := os.WriteFile(store, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(context.Background(), &fakeRunner{}, &fakeSuspend{}, nil, t.TempDir(), store)
	list := m.List()
	if len(list) != maxFinished+1 || list[0].ID != "5" || list[len(list)-1].ID != "waiting" {
		t.Fatalf("%d %s", len(list), list[0].ID)
	}
}
