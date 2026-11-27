package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
)

const (
	StateQueued           = "queued"
	StateDownloadingVideo = "downloading_video"
	StateDownloadingAudio = "downloading_audio"
	StateMuxing           = "muxing"
	StateDone             = "done"
	StatePaused           = "paused"
	StateFailed           = "failed"
	StateCancelled        = "cancelled"
)

type Job struct {
	ID             string          `json:"id"`
	Key            string          `json:"key"`
	VideoID        string          `json:"videoId"`
	URL            string          `json:"url"`
	Title          string          `json:"title"`
	Thumbnail      string          `json:"thumbnail"`
	Height         int             `json:"height"`
	Container      string          `json:"container"`
	VideoFormatID  string          `json:"videoFormatId"`
	AudioFormatID  string          `json:"audioFormatId"`
	EstimatedBytes int64           `json:"estimatedBytes"`
	State          string          `json:"state"`
	Stage          string          `json:"stage"`
	Percent        float64         `json:"percent"`
	BytesDone      int64           `json:"bytesDone"`
	BytesTotal     int64           `json:"bytesTotal"`
	SpeedBps       float64         `json:"speedBps"`
	EtaSec         *float64        `json:"etaSec"`
	OutputPath     string          `json:"outputPath,omitempty"`
	Error          *protocol.Error `json:"error,omitempty"`
	CreatedAt      string          `json:"createdAt"`
	ResumeDownload bool            `json:"resumeDownload"`
	Kind           string          `json:"kind,omitempty"`
	RecodeAudio    bool            `json:"recodeAudio,omitempty"`
	SampleRate     int             `json:"sampleRate,omitempty"`
	Attempts       int             `json:"attempts,omitempty"`
	RetryMux       bool            `json:"retryMux,omitempty"`
	// Dir is the folder the job downloads into. It is fixed when the job first
	// starts, so changing the setting doesn't strand its partial files.
	Dir string `json:"dir,omitempty"`
	// Files lists what the job has written to, so that only those are removed
	// when it is cancelled. It is saved with the queue and not sent to the browser.
	Files []string `json:"files,omitempty"`
}

type Request struct {
	URL            string `json:"url"`
	VideoID        string `json:"videoId"`
	Title          string `json:"title"`
	Thumbnail      string `json:"thumbnail"`
	Height         int    `json:"height"`
	Container      string `json:"container"`
	VideoFormatID  string `json:"videoFormatId"`
	AudioFormatID  string `json:"audioFormatId"`
	EstimatedBytes int64  `json:"estimatedBytes"`
	Kind           string `json:"kind"`
	RecodeAudio    bool   `json:"recodeAudio"`
	SampleRate     int    `json:"sampleRate"`
}

type Result struct {
	Job     Job
	Already bool
}

type Handle interface {
	PID() int
	Wait() error
	Kill() error
	OutputPath() string
}

type Runner interface {
	Start(ctx context.Context, job Job, onProgress func(media.Progress)) (Handle, error)
}

type Suspender interface {
	Suspend(pid int) error
	Resume(pid int) error
}

type Disk interface {
	FreeBytes(path string) (uint64, error)
}

type Event struct {
	Type string
	Job  Job
}

// Manager owns the queue. At most one job runs at a time: slot names the job
// that holds the queue because it is running, paused mid-download, or waiting to
// retry. A job with an entry in active has a process that hasn't exited yet.
type Manager struct {
	mu          sync.Mutex
	jobs        []*Job
	runner      Runner
	suspend     Suspender
	disk        Disk
	downloadDir string
	storePath   string
	// keepStore stops writes when the saved queue exists but couldn't be
	// read or set aside, so it is never replaced with an empty one.
	keepStore bool
	slot      string
	active    map[string]*activeRun
	running   sync.WaitGroup
	closing   bool
	onEvent   func(Event)
	now       func() time.Time
	backoff   func(int) time.Duration
	root      context.Context
}

func New(root context.Context, runner Runner, suspend Suspender, disk Disk, downloadDir, storePath string) *Manager {
	m := &Manager{
		runner:      runner,
		suspend:     suspend,
		disk:        disk,
		downloadDir: downloadDir,
		storePath:   storePath,
		active:      map[string]*activeRun{},
		now:         time.Now,
		root:        root,
	}
	if err := m.load(); err != nil {
		log.Printf("load queue: %v", err)
	}
	m.recover()
	m.sweep()
	m.prune()
	m.persist()
	return m
}

func (m *Manager) OnEvent(fn func(Event)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvent = fn
}

// List returns the queue as it is shown to the browser.
func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, len(m.jobs))
	for i, job := range m.jobs {
		out[i] = public(job)
	}
	return out
}

func (m *Manager) DownloadDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downloadDir
}

// HasOutput reports whether path is the saved file of a job in the list.
func (m *Manager) HasOutput(path string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, job := range m.jobs {
		if job.State == StateDone && job.OutputPath != "" && strings.EqualFold(filepath.Clean(job.OutputPath), filepath.Clean(path)) {
			return true
		}
	}
	return false
}

func (m *Manager) SetDownloadDir(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" || clean == "." {
		return &protocol.Error{Code: "PATH_UNWRITABLE", Message: "Choose a download folder."}
	}
	if err := os.MkdirAll(clean, 0o755); err != nil {
		return &protocol.Error{Code: "PATH_UNWRITABLE", Message: "StreamGrab can't write to that folder."}
	}
	probe, err := os.CreateTemp(clean, ".streamgrab-*")
	if err != nil {
		return &protocol.Error{Code: "PATH_UNWRITABLE", Message: "StreamGrab can't write to that folder."}
	}
	name := probe.Name()
	_ = probe.Close()
	_ = os.Remove(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downloadDir = clean
	m.persistDir()
	return nil
}

func (m *Manager) Enqueue(req Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kind := req.Kind
	if kind == "" {
		kind = "video"
	}
	container := req.Container
	if container == "" {
		container = "mp4"
	}
	key := req.VideoID + ":" + strconv.Itoa(req.Height) + ":" + container
	if kind == "audio" {
		key = req.VideoID + ":audio:" + container
	}
	for _, job := range m.jobs {
		if job.Key == key && !terminal(job.State) {
			return Result{Job: public(job), Already: true}, nil
		}
	}
	if req.EstimatedBytes > 0 && m.disk != nil {
		free, err := m.disk.FreeBytes(m.downloadDir)
		if err == nil && float64(free) < float64(req.EstimatedBytes)*1.2 {
			return Result{}, &protocol.Error{
				Code:    "DISK_FULL",
				Message: "Not enough space for this download.",
			}
		}
	}
	job := &Job{
		ID:             newID(),
		Key:            key,
		VideoID:        req.VideoID,
		URL:            req.URL,
		Title:          req.Title,
		Thumbnail:      req.Thumbnail,
		Height:         req.Height,
		Container:      container,
		Kind:           kind,
		RecodeAudio:    req.RecodeAudio,
		SampleRate:     req.SampleRate,
		VideoFormatID:  req.VideoFormatID,
		AudioFormatID:  req.AudioFormatID,
		EstimatedBytes: req.EstimatedBytes,
		State:          StateQueued,
		Stage:          StateQueued,
		CreatedAt:      m.now().UTC().Format(time.RFC3339),
	}
	m.jobs = append(m.jobs, job)
	m.changed(job)
	m.kick()
	return Result{Job: public(job)}, nil
}

// Pause suspends a running job, or stops it if it can't be suspended. A job
// that is still waiting its turn is set aside without holding up the queue.
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	if job == nil || terminal(job.State) {
		return &protocol.Error{Code: "UNKNOWN", Message: "That download is not active."}
	}
	if job.State == StatePaused {
		return nil
	}
	if run := m.active[id]; run != nil {
		suspendable := run.handle != nil && run.handle.PID() != 0
		if suspendable && m.suspend.Suspend(run.handle.PID()) == nil {
			run.suspended = true
		} else {
			run.stop()
			job.ResumeDownload = true
		}
	}
	job.State = StatePaused
	m.changed(job)
	return nil
}

func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	if job == nil || job.State != StatePaused {
		return &protocol.Error{Code: "UNKNOWN", Message: "That download is not paused."}
	}
	run := m.active[id]
	if run != nil && run.suspended && !run.stopped {
		if err := m.suspend.Resume(run.handle.PID()); err != nil {
			return &protocol.Error{Code: "UNKNOWN", Message: "StreamGrab couldn't resume the download."}
		}
		run.suspended = false
		job.State = job.Stage
		if job.State == "" || job.State == StatePaused || job.State == StateQueued {
			job.State = StateDownloadingVideo
		}
		m.changed(job)
		return nil
	}
	job.ResumeDownload = true
	job.Attempts = 0
	job.State = StateQueued
	job.Stage = StateQueued
	job.Error = nil
	// A process that is still exiting releases the queue itself.
	if run == nil {
		m.release(id)
	}
	m.changed(job)
	m.kick()
	return nil
}

func (m *Manager) ResumeQueue() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kick()
}

// Cancel stops a job and discards its partial files. Cancelling a job that has
// already failed or been cancelled removes it from the list.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	if job == nil {
		return &protocol.Error{Code: "UNKNOWN", Message: "That download is not in the queue."}
	}
	switch job.State {
	case StateDone:
		return &protocol.Error{Code: "UNKNOWN", Message: "That download already finished."}
	case StateFailed, StateCancelled:
		m.remove(job)
		m.persist()
		return nil
	}
	job.State = StateCancelled
	job.Error = &protocol.Error{Code: "CANCELLED", Message: "Download cancelled."}
	if run := m.active[id]; run != nil {
		// The files are removed once the process has let go of them.
		run.discard = true
		run.stop()
		m.changed(job)
		return nil
	}
	m.discard(job)
	m.release(id)
	m.changed(job)
	m.kick()
	return nil
}

func (m *Manager) RetryMux(id, container string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	if job == nil || job.State != StateFailed || job.Error == nil || job.Error.Code != "MUX_FAILED" {
		return &protocol.Error{Code: "MUX_FAILED", Message: "That download cannot be muxed again."}
	}
	if job.Kind == "audio" {
		// Audio has no streams to merge. yt-dlp repeats the conversion.
		job.RetryMux = false
		job.ResumeDownload = true
	} else {
		if container == "" {
			container = job.Container
		}
		switch container {
		case "mp4", "webm", "mkv":
		default:
			return &protocol.Error{Code: "UNKNOWN", Message: "Choose MP4, WebM, or MKV."}
		}
		job.Container = container
		job.RetryMux = true
	}
	job.State = StateQueued
	job.Stage = StateQueued
	job.Error = nil
	job.Percent = 0
	job.Attempts = 0
	m.release(id)
	m.changed(job)
	m.kick()
	return nil
}

// recover pauses jobs that were running when the host last stopped.
func (m *Manager) recover() {
	for _, job := range m.jobs {
		switch job.State {
		case StateDownloadingVideo, StateDownloadingAudio, StateMuxing:
			job.State = StatePaused
			job.ResumeDownload = true
			job.Stage = StatePaused
		}
	}
	m.slot = ""
	for _, job := range m.jobs {
		// A job that was set aside before it started doesn't hold the queue.
		if job.State == StatePaused && job.ResumeDownload {
			m.slot = job.ID
			break
		}
	}
}

func (m *Manager) find(id string) *Job {
	for _, job := range m.jobs {
		if job.ID == id {
			return job
		}
	}
	return nil
}

func (m *Manager) release(id string) {
	if m.slot == id {
		m.slot = ""
	}
}

// changed saves the queue and tells the browser about one job.
func (m *Manager) changed(job *Job) {
	m.persist()
	m.emit(Event{Type: "JOB_STATE", Job: public(job)})
}

func (m *Manager) emit(event Event) {
	if m.onEvent != nil {
		m.onEvent(event)
	}
}

func public(job *Job) Job {
	out := *job
	out.Files = nil
	return out
}

func terminal(state string) bool {
	return state == StateDone || state == StateFailed || state == StateCancelled
}

func newID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
