package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
	"streamgrab/host/internal/settings"
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
}

type Request struct {
	URL            string
	VideoID        string
	Title          string
	Thumbnail      string
	Height         int
	Container      string
	VideoFormatID  string
	AudioFormatID  string
	EstimatedBytes int64
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

type Manager struct {
	mu          sync.Mutex
	jobs        []*Job
	runner      Runner
	suspend     Suspender
	disk        Disk
	downloadDir string
	storePath   string
	slot        string
	active      map[string]activeRun
	onEvent     func(Event)
	onDir       func(string)
	now         func() time.Time
	backoff     func(int) time.Duration
	root        context.Context
}

type activeRun struct {
	cancel    context.CancelFunc
	handle    Handle
	suspended bool
	lastEmit  time.Time
	smoothed  *float64
}

func New(root context.Context, runner Runner, suspend Suspender, disk Disk, downloadDir, storePath string) *Manager {
	m := &Manager{
		runner:      runner,
		suspend:     suspend,
		disk:        disk,
		downloadDir: downloadDir,
		storePath:   storePath,
		active:      map[string]activeRun{},
		now:         time.Now,
		root:        root,
	}
	_ = m.load()
	m.recover()
	sweepFragments(downloadDir, 24*time.Hour, time.Now())
	return m
}

func (m *Manager) OnEvent(fn func(Event)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEvent = fn
}

func (m *Manager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshot()
}

func (m *Manager) DownloadDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.downloadDir
}

func (m *Manager) OnDownloadDir(fn func(string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDir = fn
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
	m.downloadDir = clean
	m.persistDir()
	hook := m.onDir
	m.mu.Unlock()
	if hook != nil {
		hook(clean)
	}
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
	key := req.VideoID + ":" + itoa(req.Height) + ":" + container
	if kind == "audio" {
		key = req.VideoID + ":audio:" + container
	}
	for _, job := range m.jobs {
		if job.Key == key && !terminal(job.State) {
			return Result{Job: *job, Already: true}, nil
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
	m.persist()
	m.emit(Event{Type: "JOB_STATE", Job: *job})
	m.kick()
	return Result{Job: *job}, nil
}

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	job := m.find(id)
	if job == nil || terminal(job.State) {
		m.mu.Unlock()
		return &protocol.Error{Code: "UNKNOWN", Message: "That download is not active."}
	}
	run := m.active[id]
	job.State = StatePaused
	m.slot = id
	m.persist()
	m.emit(Event{Type: "JOB_STATE", Job: *job})
	m.mu.Unlock()

	if run.handle == nil {
		m.mu.Lock()
		if current := m.find(id); current != nil {
			current.ResumeDownload = true
			m.persist()
		}
		m.mu.Unlock()
		return nil
	}
	if err := m.suspend.Suspend(run.handle.PID()); err != nil {
		m.mu.Lock()
		if current := m.find(id); current != nil {
			current.ResumeDownload = true
		}
		delete(m.active, id)
		m.persist()
		m.mu.Unlock()
		_ = run.handle.Kill()
		return nil
	}
	m.mu.Lock()
	if current, ok := m.active[id]; ok {
		current.suspended = true
		m.active[id] = current
	}
	m.mu.Unlock()
	return nil
}

func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	if job == nil || job.State != StatePaused {
		return &protocol.Error{Code: "UNKNOWN", Message: "That download is not paused."}
	}
	if run, ok := m.active[id]; ok && run.suspended && run.handle != nil {
		if err := m.suspend.Resume(run.handle.PID()); err != nil {
			return &protocol.Error{Code: "UNKNOWN", Message: "StreamGrab couldn't resume the download."}
		}
		run.suspended = false
		m.active[id] = run
		job.State = job.Stage
		if job.State == "" || job.State == StatePaused || job.State == StateQueued {
			job.State = StateDownloadingVideo
		}
		m.persist()
		m.emit(Event{Type: "JOB_STATE", Job: *job})
		return nil
	}
	job.ResumeDownload = true
	job.Attempts = 0
	job.State = StateQueued
	if m.slot == id {
		m.slot = ""
	}
	m.persist()
	m.kick()
	return nil
}

func (m *Manager) ResumeQueue() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kick()
}

func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	job := m.find(id)
	if job == nil {
		m.mu.Unlock()
		return &protocol.Error{Code: "UNKNOWN", Message: "That download is not in the queue."}
	}
	if job.State == StateDone {
		m.mu.Unlock()
		return &protocol.Error{Code: "UNKNOWN", Message: "That download already finished."}
	}
	run := m.active[id]
	delete(m.active, id)
	if m.slot == id {
		m.slot = ""
	}
	job.State = StateCancelled
	job.Error = &protocol.Error{Code: "CANCELLED", Message: "Download cancelled."}
	m.persist()
	copied := *job
	m.emit(Event{Type: "JOB_STATE", Job: copied})
	m.kick()
	m.mu.Unlock()
	if run.cancel != nil {
		run.cancel()
	}
	if run.handle != nil {
		_ = run.handle.Kill()
	}
	cleanParts(m.downloadDir, copied.VideoID)
	return nil
}

func (m *Manager) kick() {
	if m.slot != "" {
		return
	}
	for _, job := range m.jobs {
		if job.State == StateQueued {
			m.start(job)
			return
		}
	}
}

func (m *Manager) start(job *Job) {
	ctx, cancel := context.WithCancel(m.root)
	if job.Kind == "audio" {
		job.State = StateDownloadingAudio
		job.Stage = StateDownloadingAudio
	} else {
		job.State = StateDownloadingVideo
		job.Stage = StateDownloadingVideo
	}
	m.slot = job.ID
	m.active[job.ID] = activeRun{cancel: cancel}
	m.persist()
	m.emit(Event{Type: "JOB_STATE", Job: *job})
	go m.run(job.ID, ctx, cancel)
}

func (m *Manager) run(id string, ctx context.Context, cancel context.CancelFunc) {
	m.mu.Lock()
	job := m.find(id)
	if job == nil {
		m.mu.Unlock()
		cancel()
		return
	}
	snapshot := *job
	m.mu.Unlock()

	handle, err := m.runner.Start(ctx, snapshot, func(progress media.Progress) {
		m.onProgress(id, progress)
	})
	if err != nil {
		m.finish(id, err, "")
		return
	}
	m.mu.Lock()
	if run, ok := m.active[id]; ok {
		run.handle = handle
		m.active[id] = run
	}
	m.mu.Unlock()
	err = handle.Wait()
	path := handle.OutputPath()
	m.finish(id, err, path)
}

func (m *Manager) onProgress(id string, progress media.Progress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	run, ok := m.active[id]
	if job == nil || !ok || job.State == StatePaused || job.State == StateCancelled {
		return
	}
	stage := progress.Stage
	if stage == "" {
		stage = job.Stage
	}
	previous := job.Stage
	job.Stage = stage
	if stage == StateMuxing {
		job.State = StateMuxing
	} else if stage == StateDownloadingAudio || job.Kind == "audio" {
		job.State = StateDownloadingAudio
	} else if job.State != StatePaused {
		job.State = StateDownloadingVideo
	}
	job.Percent = progress.Percent
	job.BytesDone = progress.BytesDone
	job.BytesTotal = progress.BytesTotal
	job.SpeedBps = progress.SpeedBps
	job.EtaSec = smooth(&run, progress.EtaSec)
	if progress.OutputPath != "" {
		job.OutputPath = progress.OutputPath
	}
	m.active[id] = run
	now := m.now()
	if !run.lastEmit.IsZero() && now.Sub(run.lastEmit) < 250*time.Millisecond && stage == previous {
		return
	}
	run.lastEmit = now
	m.active[id] = run
	m.emit(Event{Type: "JOB_PROGRESS", Job: *job})
}

func (m *Manager) finish(id string, err error, path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	delete(m.active, id)
	if job == nil || job.State == StateCancelled || job.State == StatePaused {
		if m.slot == id && (job == nil || job.State != StatePaused) {
			m.slot = ""
		}
		m.persist()
		m.kick()
		return
	}
	if err == nil && path != "" {
		if info, statErr := os.Stat(path); statErr == nil && info.Size() == 0 {
			_ = os.Remove(path)
			err = &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept."}
			path = ""
		}
	}
	if path != "" {
		job.OutputPath = path
	}
	if m.slot == id {
		m.slot = ""
	}
	if err != nil {
		code, message := classify(err)
		if code == "NETWORK_ERROR" && job.Attempts < 3 {
			job.Attempts++
			attempt := job.Attempts
			job.State = StateQueued
			job.Stage = StateQueued
			job.ResumeDownload = true
			job.Error = nil
			m.persist()
			m.emit(Event{Type: "JOB_STATE", Job: *job})
			delay := m.retryAfter(attempt)
			go func() {
				time.Sleep(delay)
				m.mu.Lock()
				defer m.mu.Unlock()
				if current := m.find(id); current != nil && current.State == StateQueued {
					m.kick()
				}
			}()
			return
		}
		if code == "NETWORK_ERROR" {
			job.State = StatePaused
			job.Stage = StatePaused
			job.ResumeDownload = true
			job.Error = &protocol.Error{Code: "NETWORK_ERROR", Message: "The download paused because the network failed."}
			m.slot = id
			m.persist()
			m.emit(Event{Type: "JOB_STATE", Job: *job})
			return
		}
		job.State = StateFailed
		job.Error = &protocol.Error{Code: code, Message: message}
	} else {
		job.State = StateDone
		job.Stage = StateDone
		job.Percent = 100
		job.Error = nil
		job.RetryMux = false
		job.Attempts = 0
	}
	m.persist()
	m.emit(Event{Type: "JOB_STATE", Job: *job})
	m.kick()
}

func (m *Manager) RetryMux(id, container string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job := m.find(id)
	if job == nil || job.State != StateFailed || job.Error == nil || job.Error.Code != "MUX_FAILED" {
		return &protocol.Error{Code: "MUX_FAILED", Message: "That download cannot be muxed again."}
	}
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
	job.State = StateQueued
	job.Stage = StateQueued
	job.Error = nil
	job.Percent = 0
	if m.slot == id {
		m.slot = ""
	}
	m.persist()
	m.emit(Event{Type: "JOB_STATE", Job: *job})
	m.kick()
	return nil
}

func (m *Manager) retryAfter(attempt int) time.Duration {
	if m.backoff != nil {
		return m.backoff(attempt)
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 3 {
		attempt = 3
	}
	return time.Duration(1<<(attempt-1)) * time.Second
}

func (m *Manager) recover() {
	for _, job := range m.jobs {
		switch job.State {
		case StateDownloadingVideo, StateDownloadingAudio, StateMuxing, StateQueued:
			if job.State != StateQueued {
				job.State = StatePaused
				job.ResumeDownload = true
				job.Stage = StatePaused
			}
		}
	}
	m.slot = ""
	for _, job := range m.jobs {
		if job.State == StatePaused {
			m.slot = job.ID
			break
		}
	}
	m.persist()
}

func (m *Manager) find(id string) *Job {
	for _, job := range m.jobs {
		if job.ID == id {
			return job
		}
	}
	return nil
}

func (m *Manager) snapshot() []Job {
	out := make([]Job, len(m.jobs))
	for i, job := range m.jobs {
		out[i] = *job
	}
	return out
}

func (m *Manager) emit(event Event) {
	if m.onEvent != nil {
		m.onEvent(event)
	}
}

func (m *Manager) persistDir() {
	if m.storePath == "" {
		return
	}
	dir := m.downloadDir
	_ = settings.Update(settings.PathBeside(m.storePath), func(file *settings.File) {
		file.DownloadDir = dir
	})
}

func (m *Manager) persist() {
	if m.storePath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(m.storePath), 0o755)
	raw, err := json.MarshalIndent(m.snapshot(), "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.storePath, raw, 0o644)
}

func (m *Manager) load() error {
	if m.storePath == "" {
		return nil
	}
	raw, err := os.ReadFile(m.storePath)
	if err != nil {
		return err
	}
	var jobs []Job
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return err
	}
	m.jobs = make([]*Job, len(jobs))
	for i := range jobs {
		job := jobs[i]
		m.jobs[i] = &job
	}
	return nil
}

func terminal(state string) bool {
	return state == StateDone || state == StateFailed || state == StateCancelled
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

func classify(err error) (string, string) {
	if api, ok := err.(*protocol.Error); ok {
		return api.Code, api.Message
	}
	text := err.Error()
	switch {
	case contains(text, "mux"):
		return "MUX_FAILED", "Muxing failed. The partial streams were kept."
	case contains(text, "disk"), contains(text, "space"):
		return "DISK_FULL", "The download disk is full."
	case contains(text, "network"), contains(text, "timed out"):
		return "NETWORK_ERROR", "The download stopped because the network failed."
	default:
		return "EXTRACTION_FAILED", "The download failed. Update yt-dlp and try again."
	}
}

func contains(s, part string) bool {
	return len(s) >= len(part) && (s == part || indexFold(s, part))
}

func indexFold(s, part string) bool {
	return len(part) > 0 && len(s) >= len(part) && (stringContainsFold(s, part))
}

func stringContainsFold(s, part string) bool {
	ls := toLower(s)
	return len(ls) >= len(part) && containsLower(ls, toLower(part))
}

func containsLower(s, part string) bool {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func cleanParts(dir, videoID string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(videoID) > 0 && containsLower(toLower(name), toLower(videoID)) && containsLower(toLower(name), ".part") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

func newID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [16]byte
	i := len(digits)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}
