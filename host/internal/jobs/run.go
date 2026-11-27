package jobs

import (
	"context"
	"errors"
	"os"
	"slices"
	"time"

	"streamgrab/host/internal/failure"
	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
)

const (
	maxRetries       = 3
	progressInterval = 250 * time.Millisecond
	shutdownWait     = 3 * time.Second
)

// activeRun is a job's process, from the moment it is requested until it exits.
type activeRun struct {
	job       *Job
	cancel    context.CancelFunc
	handle    Handle
	suspended bool
	// stopped marks a run that was ended on purpose, so its result is ignored.
	stopped bool
	// discard removes the job's files after the process exits.
	discard  bool
	lastEmit time.Time
	smoothed *float64
}

func (r *activeRun) stop() {
	r.stopped = true
	r.cancel()
	if r.handle != nil {
		_ = r.handle.Kill()
	}
}

func (m *Manager) kick() {
	if m.slot != "" || m.closing {
		return
	}
	for _, job := range m.jobs {
		// A job whose previous process is still exiting has to wait for it.
		if job.State == StateQueued && m.active[job.ID] == nil {
			m.start(job)
			return
		}
	}
}

func (m *Manager) start(job *Job) {
	ctx, cancel := context.WithCancel(m.root)
	if job.Kind == "audio" {
		job.State = StateDownloadingAudio
	} else {
		job.State = StateDownloadingVideo
	}
	job.Stage = job.State
	if job.Dir == "" {
		job.Dir = m.downloadDir
	}
	run := &activeRun{job: job, cancel: cancel}
	m.slot = job.ID
	m.active[job.ID] = run
	m.changed(job)
	snapshot := *job
	snapshot.Files = slices.Clone(job.Files)
	m.running.Add(1)
	go m.run(ctx, run, snapshot)
}

func (m *Manager) run(ctx context.Context, run *activeRun, snapshot Job) {
	defer m.running.Done()
	defer run.cancel()
	handle, err := m.runner.Start(ctx, snapshot, func(progress media.Progress) {
		m.onProgress(run, progress)
	})
	if err != nil {
		m.finish(run, err, "")
		return
	}
	m.mu.Lock()
	run.handle = handle
	if run.stopped {
		_ = handle.Kill()
	}
	m.mu.Unlock()
	err = handle.Wait()
	m.finish(run, err, handle.OutputPath())
}

func (m *Manager) onProgress(run *activeRun, progress media.Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := run.job
	if m.closing || m.active[job.ID] != run {
		return
	}
	// Remember files even for a stopped run, so that they can be removed.
	if progress.File != "" && !slices.Contains(job.Files, progress.File) {
		job.Files = append(job.Files, progress.File)
		m.persist()
	}
	if run.stopped || job.State == StatePaused {
		return
	}
	stage := progress.Stage
	if stage == "" {
		stage = job.Stage
	}
	previous := job.Stage
	job.Stage = stage
	switch {
	case stage == StateMuxing:
		job.State = StateMuxing
	case stage == StateDownloadingAudio || job.Kind == "audio":
		job.State = StateDownloadingAudio
	default:
		job.State = StateDownloadingVideo
	}
	job.Percent = progress.Percent
	job.BytesDone = progress.BytesDone
	job.BytesTotal = progress.BytesTotal
	job.SpeedBps = progress.SpeedBps
	job.EtaSec = smooth(run, progress.EtaSec)
	if progress.OutputPath != "" {
		job.OutputPath = progress.OutputPath
	}
	now := m.now()
	if !run.lastEmit.IsZero() && now.Sub(run.lastEmit) < progressInterval && stage == previous {
		return
	}
	run.lastEmit = now
	m.emit(Event{Type: "JOB_PROGRESS", Job: public(job)})
}

// finish handles the exit of a job's process.
func (m *Manager) finish(run *activeRun, err error, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// When the host is closing, the saved state stays "downloading" so that the
	// next start offers to resume.
	if m.closing {
		return
	}
	job := run.job
	id := job.ID
	delete(m.active, id)
	if run.discard {
		m.discard(job)
	}
	if run.stopped || job.State == StatePaused || job.State == StateCancelled {
		// A paused job keeps its place. Anything else frees the queue.
		if job.State != StatePaused {
			m.release(id)
		} else {
			job.ResumeDownload = true
		}
		m.persist()
		m.kick()
		return
	}
	if err == nil && path != "" {
		if empty(path) {
			err = &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept."}
			path = ""
		}
	}
	if path != "" {
		job.OutputPath = path
	}
	if err == nil {
		job.State = StateDone
		job.Stage = StateDone
		job.Percent = 100
		job.Error = nil
		job.RetryMux = false
		job.Attempts = 0
		job.Files = nil
	} else {
		failed := asFailure(err)
		if failed.Code == "NETWORK_ERROR" {
			m.retryOrPause(job)
			return
		}
		job.State = StateFailed
		job.Error = failed
	}
	m.release(id)
	m.changed(job)
	m.prune()
	m.persist()
	m.kick()
}

// retryOrPause keeps the job's place in the queue while it waits to try again,
// and pauses it once the retries are used up.
func (m *Manager) retryOrPause(job *Job) {
	job.ResumeDownload = true
	m.slot = job.ID
	if job.Attempts >= maxRetries {
		job.State = StatePaused
		job.Stage = StatePaused
		job.Error = &protocol.Error{Code: "NETWORK_ERROR", Message: "The download paused because the network failed."}
		m.changed(job)
		return
	}
	job.Attempts++
	attempt := job.Attempts
	job.State = StateQueued
	job.Stage = StateQueued
	job.Error = nil
	m.changed(job)
	time.AfterFunc(m.retryAfter(attempt), func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		// The user may have paused, cancelled, or resumed the job meanwhile.
		if m.closing || m.slot != job.ID || job.State != StateQueued || job.Attempts != attempt || m.active[job.ID] != nil {
			return
		}
		m.start(job)
	})
}

// Shutdown stops running processes without changing the saved queue.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closing = true
	for _, run := range m.active {
		run.stop()
	}
	m.mu.Unlock()
	done := make(chan struct{})
	go func() {
		m.running.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownWait):
	}
}

func (m *Manager) retryAfter(attempt int) time.Duration {
	if m.backoff != nil {
		return m.backoff(attempt)
	}
	attempt = min(max(attempt, 1), maxRetries)
	return time.Duration(1<<(attempt-1)) * time.Second
}

func smooth(run *activeRun, next *float64) *float64 {
	if next == nil || *next <= 0 {
		return nil
	}
	value := *next
	if run.smoothed != nil {
		value = *run.smoothed*0.7 + value*0.3
	}
	run.smoothed = &value
	return &value
}

func asFailure(err error) *protocol.Error {
	var api *protocol.Error
	if errors.As(err, &api) {
		return api
	}
	return failure.Classify(err.Error())
}

// empty reports whether path is a file with no content, and removes it.
func empty(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() != 0 {
		return false
	}
	_ = os.Remove(path)
	return true
}
