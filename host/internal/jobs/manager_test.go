package jobs

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
)

type fakeDisk struct {
	free uint64
}

func (d fakeDisk) FreeBytes(string) (uint64, error) { return d.free, nil }

type fakeSuspend struct {
	mu        sync.Mutex
	suspended map[int]bool
	fail      bool
}

func (s *fakeSuspend) Suspend(pid int) error {
	if s.fail {
		return errors.New("suspend unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.suspended == nil {
		s.suspended = map[int]bool{}
	}
	s.suspended[pid] = true
	return nil
}

func (s *fakeSuspend) Resume(pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.suspended, pid)
	return nil
}

type fakeHandle struct {
	pid    int
	done   chan error
	killed chan struct{}
	once   sync.Once
	path   string
}

func (h *fakeHandle) PID() int           { return h.pid }
func (h *fakeHandle) OutputPath() string { return h.path }
func (h *fakeHandle) Wait() error {
	select {
	case err := <-h.done:
		return err
	case <-h.killed:
		return errors.New("killed")
	}
}
func (h *fakeHandle) Kill() error {
	h.once.Do(func() { close(h.killed) })
	return nil
}

type fakeRunner struct {
	mu       sync.Mutex
	starts   int
	handle   *fakeHandle
	progress media.Progress
}

func (r *fakeRunner) Start(ctx context.Context, job Job, onProgress func(media.Progress)) (Handle, error) {
	r.mu.Lock()
	r.starts++
	h := &fakeHandle{pid: 42 + r.starts, done: make(chan error, 1), killed: make(chan struct{}), path: filepath.Join("out", job.VideoID+".mp4")}
	r.handle = h
	progress := r.progress
	r.mu.Unlock()
	go func() {
		if onProgress != nil && progress.Stage != "" {
			onProgress(progress)
		}
		<-ctx.Done()
	}()
	return h, nil
}

func TestEnqueueDuplicateAndDisk(t *testing.T) {
	runner := &fakeRunner{progress: media.Progress{Stage: StateDownloadingVideo, Percent: 10, BytesDone: 10, BytesTotal: 100}}
	manager := New(context.Background(), runner, &fakeSuspend{}, fakeDisk{free: 10_000}, t.TempDir(), "")
	req := Request{URL: "https://www.youtube.com/watch?v=abcdefghijk", VideoID: "abcdefghijk", Height: 1080, EstimatedBytes: 1000, VideoFormatID: "137", AudioFormatID: "140", Title: "Example"}
	first, err := manager.Enqueue(req)
	if err != nil || first.Already {
		t.Fatalf("first = %+v %v", first, err)
	}
	second, err := manager.Enqueue(req)
	if err != nil || !second.Already || second.Job.ID != first.Job.ID {
		t.Fatalf("second = %+v %v", second, err)
	}
	tight := New(context.Background(), &fakeRunner{}, &fakeSuspend{}, fakeDisk{free: 100}, t.TempDir(), "")
	_, err = tight.Enqueue(req)
	api, ok := err.(*protocol.Error)
	if !errOK(ok) || api.Code != "DISK_FULL" {
		t.Fatalf("disk error = %v", err)
	}
}

func TestPauseKeepsSlot(t *testing.T) {
	runner := &fakeRunner{}
	suspend := &fakeSuspend{}
	manager := New(context.Background(), runner, suspend, fakeDisk{free: 1 << 30}, t.TempDir(), "")
	first, err := manager.Enqueue(Request{VideoID: "abcdefghijk", Height: 720, EstimatedBytes: 10, VideoFormatID: "136"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return runner.handle != nil
	})
	if err := manager.Pause(first.Job.ID); err != nil {
		t.Fatal(err)
	}
	other, err := manager.Enqueue(Request{VideoID: "zzzzzzzzzzz", Height: 720, EstimatedBytes: 10, VideoFormatID: "136"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if other.Job.State != StateQueued {
		t.Fatalf("second job state = %s", other.Job.State)
	}
	runner.mu.Lock()
	starts := runner.starts
	runner.mu.Unlock()
	if starts != 1 {
		t.Fatalf("starts = %d", starts)
	}
	if err := manager.Resume(first.Job.ID); err != nil {
		t.Fatal(err)
	}
	jobs := manager.List()
	if jobs[0].State == StatePaused {
		t.Fatalf("resumed state = %s", jobs[0].State)
	}
	_ = suspend
}

func TestCompletesAndStartsNext(t *testing.T) {
	runner := &fakeRunner{}
	manager := New(context.Background(), runner, &fakeSuspend{}, fakeDisk{free: 1 << 30}, t.TempDir(), "")
	done := make(chan Event, 4)
	manager.OnEvent(func(event Event) {
		if event.Type == "JOB_STATE" && event.Job.State == StateDone {
			done <- event
		}
	})
	first, err := manager.Enqueue(Request{VideoID: "abcdefghijk", Height: 1080, VideoFormatID: "137"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return runner.handle != nil
	})
	runner.mu.Lock()
	handle := runner.handle
	runner.mu.Unlock()
	handle.done <- nil
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for completion")
	}
	if _, err := manager.Enqueue(Request{VideoID: "bbbbbbbbbbb", Height: 720, VideoFormatID: "136"}); err != nil {
		t.Fatal(err)
	}
	_ = first
}

func TestRecoverMarksInFlightPaused(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "jobs.json")
	runner := &fakeRunner{}
	manager := New(context.Background(), runner, &fakeSuspend{}, fakeDisk{free: 1 << 30}, dir, store)
	_, err := manager.Enqueue(Request{VideoID: "abcdefghijk", Height: 720, VideoFormatID: "136"})
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		runner.mu.Lock()
		defer runner.mu.Unlock()
		return runner.starts == 1
	})
	reloaded := New(context.Background(), &fakeRunner{}, &fakeSuspend{}, fakeDisk{free: 1 << 30}, dir, store)
	jobs := reloaded.List()
	if len(jobs) != 1 || jobs[0].State != StatePaused || !jobs[0].ResumeDownload {
		t.Fatalf("reloaded = %+v", jobs)
	}
}

func TestNetworkRetriesThenPauses(t *testing.T) {
	runner := &failRunner{err: errors.New("network down")}
	manager := New(context.Background(), runner, &fakeSuspend{}, fakeDisk{free: 1 << 30}, t.TempDir(), "")
	manager.backoff = func(int) time.Duration { return time.Millisecond }
	if _, err := manager.Enqueue(Request{VideoID: "abcdefghijk", Height: 720, VideoFormatID: "136"}); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		jobs := manager.List()
		return len(jobs) == 1 && jobs[0].State == StatePaused && jobs[0].Error != nil && jobs[0].Error.Code == "NETWORK_ERROR"
	})
	runner.mu.Lock()
	starts := runner.starts
	runner.mu.Unlock()
	if starts != 4 {
		t.Fatalf("starts = %d", starts)
	}
}

type failRunner struct {
	mu     sync.Mutex
	starts int
	err    error
}

func (r *failRunner) Start(context.Context, Job, func(media.Progress)) (Handle, error) {
	r.mu.Lock()
	r.starts++
	err := r.err
	r.mu.Unlock()
	return nil, err
}

func waitUntil(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met")
}

func errOK(ok bool) bool { return ok }
