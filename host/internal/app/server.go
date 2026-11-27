package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"streamgrab/host/internal/jobs"
	"streamgrab/host/internal/media"
	"streamgrab/host/internal/platform"
	"streamgrab/host/internal/protocol"
	"streamgrab/host/internal/settings"
	"streamgrab/host/internal/update"
	"streamgrab/host/internal/youtube"
	"streamgrab/host/internal/ytdlp"
)

type Server struct {
	In           io.Reader
	Out          io.Writer
	Jobs         *jobs.Manager
	Info         func(context.Context, string) (media.Probe, error)
	Versions     protocol.Handshake
	Runner       *ytdlp.Runner
	Updater      update.Client
	CoreDir      string
	SettingsPath string
	AutoUpdate   bool
	// mu guards Versions and AutoUpdate once the server is running.
	mu           sync.Mutex
	writeMu      sync.Mutex
	lookupMu     sync.Mutex
	cancelLookup context.CancelFunc
	updateMu     sync.Mutex
	coreOnce     sync.Once
}

func (s *Server) Serve(ctx context.Context) error {
	s.Jobs.OnEvent(func(event jobs.Event) {
		_ = s.write(protocol.Outgoing{
			ID:      event.Job.ID,
			Type:    event.Type,
			OK:      true,
			Payload: eventPayload(event),
		})
	})
	s.Jobs.ResumeQueue()
	go s.scheduleCoreUpdate(ctx)
	for {
		raw, err := protocol.Read(s.In)
		if err != nil {
			return err
		}
		msg, err := protocol.DecodeIncoming(raw)
		if err != nil {
			continue
		}
		// Queue commands are quick and are handled in the order they arrive.
		switch msg.Type {
		case "ENQUEUE", "PAUSE", "RESUME", "CANCEL", "RETRY_MUX":
			s.dispatch(ctx, msg)
		default:
			go s.dispatch(ctx, msg)
		}
	}
}

func (s *Server) dispatch(ctx context.Context, msg protocol.Incoming) {
	switch msg.Type {
	case "HANDSHAKE":
		s.reply(msg, s.handshake(), nil)
		go s.scheduleCoreUpdate(ctx)
	case "UPDATE_CORE":
		s.updateCore(ctx, msg)
	case "SET_AUTO_UPDATE":
		s.setAutoUpdate(ctx, msg)
	case "RETRY_MUX":
		s.retryMux(msg)
	case "GET_INFO":
		s.getInfo(ctx, msg)
	case "ENQUEUE":
		s.enqueue(msg)
	case "PAUSE":
		s.jobAction(msg, s.Jobs.Pause)
	case "RESUME":
		s.jobAction(msg, s.Jobs.Resume)
	case "CANCEL":
		s.jobAction(msg, s.Jobs.Cancel)
	case "LIST_JOBS":
		s.reply(msg, map[string]any{"jobs": s.Jobs.List()}, nil)
	case "GET_DISK_INFO":
		free, err := platform.FreeBytes(s.Jobs.DownloadDir())
		if err != nil {
			s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "StreamGrab couldn't read the disk."})
			return
		}
		s.reply(msg, map[string]any{"freeBytes": free, "path": s.Jobs.DownloadDir()}, nil)
	case "OPEN_FOLDER":
		s.reply(msg, map[string]any{"path": s.Jobs.DownloadDir()}, platformError(platform.OpenFolder(s.Jobs.DownloadDir())))
	case "PICK_DIRECTORY":
		s.pickDirectory(msg)
	case "SHOW_IN_FOLDER":
		var body struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal(msg.Payload, &body)
		// Downloads made before the folder was changed are still ours to show.
		if !inside(s.Jobs.DownloadDir(), body.Path) && !s.Jobs.HasOutput(body.Path) {
			s.reply(msg, nil, &protocol.Error{Code: "PATH_UNWRITABLE", Message: "That file is outside the download folder."})
			return
		}
		s.reply(msg, map[string]any{"path": body.Path}, platformError(platform.ShowInFolder(body.Path)))
	default:
		s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "Unknown request."})
	}
}

func (s *Server) getInfo(ctx context.Context, msg protocol.Incoming) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(msg.Payload, &body); err != nil {
		s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "Missing video URL."})
		return
	}
	parsed, err := youtube.Parse(body.URL)
	if err != nil {
		s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "Open a YouTube video to get started."})
		return
	}
	infoCtx, cancel := s.beginLookup(ctx)
	defer cancel()
	probe, err := s.readInfo(infoCtx, parsed.URL)
	if err != nil {
		s.reply(msg, nil, asAPI(err))
		return
	}
	if probe.IsLive {
		s.reply(msg, nil, &protocol.Error{Code: "LIVE_STREAM", Message: "This live stream can't be downloaded in this version."})
		return
	}
	if drmOnly(probe) {
		s.reply(msg, nil, &protocol.Error{Code: "DRM_PROTECTED", Message: "StreamGrab doesn't support DRM content."})
		return
	}
	built := media.Build(probe)
	if probe.AgeLimit > 0 && len(built.Rows) == 0 {
		s.reply(msg, nil, &protocol.Error{Code: "AGE_RESTRICTED", Message: "This video is age-restricted. Browser cookies are not enabled in this version."})
		return
	}
	s.reply(msg, map[string]any{
		"id":          first(probe.ID, parsed.VideoID),
		"url":         parsed.URL,
		"title":       probe.Title,
		"channel":     probe.Channel,
		"thumbnail":   probe.Thumbnail,
		"durationSec": int64(probe.Duration),
		"viewCount":   probe.ViewCount,
		"kind":        parsed.Kind,
		"rows":        built.Rows,
		"chips":       built.Chips,
		"audio":       built.Audio,
		"presets":     built.Presets,
	}, nil)
}

func (s *Server) enqueue(msg protocol.Incoming) {
	var body jobs.Request
	if err := json.Unmarshal(msg.Payload, &body); err != nil {
		s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "Missing download request."})
		return
	}
	parsed, err := youtube.Parse(body.URL)
	if err != nil {
		s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "Open a YouTube video to get started."})
		return
	}
	if body.Kind == "" {
		body.Kind = "video"
	}
	if body.Kind == "audio" {
		if !allowedAudio(body.Container) || !youtube.ValidFormatID(body.AudioFormatID) || body.AudioFormatID == "" {
			s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "That audio format can't be downloaded."})
			return
		}
		body.Height = 0
		body.VideoFormatID = ""
	} else {
		if body.Container == "" {
			body.Container = "mp4"
		}
		if !allowedHeight(body.Height) || !allowedContainer(body.Container) {
			s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "Choose a 720p to 4K tier in MP4, WebM, or MKV."})
			return
		}
		if !youtube.ValidFormatID(body.VideoFormatID) || !youtube.ValidFormatID(body.AudioFormatID) || body.VideoFormatID == "" {
			s.reply(msg, nil, &protocol.Error{Code: "UNKNOWN", Message: "That format can't be downloaded."})
			return
		}
		body.Kind = "video"
	}
	body.URL = parsed.URL
	body.VideoID = parsed.VideoID
	result, err := s.Jobs.Enqueue(body)
	if err != nil {
		s.reply(msg, nil, asAPI(err))
		return
	}
	s.reply(msg, map[string]any{"job": result.Job, "already": result.Already}, nil)
}

func (s *Server) pickDirectory(msg protocol.Incoming) {
	path, err := platform.PickDirectory()
	if errors.Is(err, platform.ErrCancelled) {
		s.reply(msg, map[string]any{"cancelled": true}, nil)
		return
	}
	if err != nil {
		s.reply(msg, nil, &protocol.Error{Code: "PATH_UNWRITABLE", Message: "StreamGrab couldn't open the folder picker."})
		return
	}
	if err := s.Jobs.SetDownloadDir(path); err != nil {
		s.reply(msg, nil, asAPI(err))
		return
	}
	dir := s.Jobs.DownloadDir()
	free, _ := platform.FreeBytes(dir)
	s.reply(msg, map[string]any{"path": dir, "freeBytes": free}, nil)
}

func (s *Server) jobAction(msg protocol.Incoming, fn func(string) error) {
	var body struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(msg.Payload, &body)
	if err := fn(body.JobID); err != nil {
		s.reply(msg, nil, asAPI(err))
		return
	}
	s.reply(msg, map[string]any{"jobId": body.JobID}, nil)
}

func (s *Server) reply(msg protocol.Incoming, payload any, err error) {
	out := protocol.Outgoing{ID: msg.ID, Type: msg.Type, OK: err == nil, Payload: payload}
	if err != nil {
		out.Error = asAPI(err)
		out.Payload = nil
	}
	if errors.Is(s.write(out), protocol.ErrTooLarge) {
		_ = s.write(protocol.Outgoing{ID: msg.ID, Type: msg.Type, Error: &protocol.Error{Code: "UNKNOWN", Message: "The reply was too large for the browser."}})
	}
}

func (s *Server) write(msg protocol.Outgoing) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return protocol.WriteMessage(s.Out, msg)
}

func (s *Server) handshake() protocol.Handshake {
	dir := s.Jobs.DownloadDir()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.Versions
	out.DownloadDir = dir
	return out
}

// beginLookup starts a video lookup and cancels the one before it, because the
// popup only ever shows the latest video.
func (s *Server) beginLookup(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	s.lookupMu.Lock()
	defer s.lookupMu.Unlock()
	if s.cancelLookup != nil {
		s.cancelLookup()
	}
	s.cancelLookup = cancel
	return ctx, cancel
}

func eventPayload(event jobs.Event) any {
	if event.Type == "JOB_REMOVED" {
		return map[string]any{"jobId": event.Job.ID}
	}
	if event.Type == "JOB_PROGRESS" {
		return protocol.ProgressPayload{
			JobID:      event.Job.ID,
			Stage:      event.Job.Stage,
			Percent:    event.Job.Percent,
			BytesDone:  event.Job.BytesDone,
			BytesTotal: event.Job.BytesTotal,
			SpeedBps:   event.Job.SpeedBps,
			EtaSec:     event.Job.EtaSec,
		}
	}
	return map[string]any{"jobId": event.Job.ID, "state": event.Job.State, "job": event.Job}
}

func asAPI(err error) *protocol.Error {
	var api *protocol.Error
	if errors.As(err, &api) {
		return api
	}
	return &protocol.Error{Code: "UNKNOWN", Message: err.Error()}
}

func platformError(err error) *protocol.Error {
	if err == nil {
		return nil
	}
	return &protocol.Error{Code: "PATH_UNWRITABLE", Message: "StreamGrab couldn't open that folder."}
}

func inside(root, target string) bool {
	if root == "" || target == "" {
		return false
	}
	cleanRoot := filepath.Clean(root)
	cleanTarget := filepath.Clean(target)
	rel, err := filepath.Rel(cleanRoot, cleanTarget)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func allowedHeight(height int) bool {
	switch height {
	case 720, 1080, 1440, 2160:
		return true
	default:
		return false
	}
}

func allowedContainer(container string) bool {
	switch container {
	case "mp4", "webm", "mkv":
		return true
	default:
		return false
	}
}

func allowedAudio(container string) bool {
	switch container {
	case "flac", "mp3", "opus", "m4a":
		return true
	default:
		return false
	}
}

func drmOnly(probe media.Probe) bool {
	sawVideo := false
	for _, format := range probe.Formats {
		if format.VCodec == "" || format.VCodec == "none" || format.Height == 0 {
			continue
		}
		sawVideo = true
		if !format.HasDRM {
			return false
		}
	}
	return sawVideo
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Server) scheduleCoreUpdate(ctx context.Context) {
	if s.CoreDir == "" {
		return
	}
	s.coreOnce.Do(func() {
		go s.publishCoreUpdate(ctx, false)
	})
}

func (s *Server) updateCore(ctx context.Context, msg protocol.Incoming) {
	result, warning, _ := s.runCoreUpdate(ctx, true)
	payload := map[string]any{
		"updated":      result.Updated && warning == "",
		"ytDlpVersion": s.version(),
		"warning":      warning,
	}
	s.reply(msg, payload, nil)
}

func (s *Server) publishCoreUpdate(ctx context.Context, force bool) {
	result, warning, ran := s.runCoreUpdate(ctx, force)
	if !ran {
		return
	}
	_ = s.write(protocol.Outgoing{
		ID:   "core",
		Type: "CORE_STATUS",
		OK:   warning == "",
		Payload: map[string]any{
			"updated":      result.Updated && warning == "",
			"ytDlpVersion": s.version(),
			"warning":      warning,
		},
	})
}

func (s *Server) runCoreUpdate(ctx context.Context, force bool) (update.Result, string, bool) {
	if s.CoreDir == "" {
		return update.Result{}, "", false
	}
	s.mu.Lock()
	enabled := s.AutoUpdate
	s.mu.Unlock()
	if !enabled && !force {
		return update.Result{}, "", false
	}
	if !force {
		file := settings.Load(s.SettingsPath)
		if !file.LastCoreCheck.IsZero() && time.Since(file.LastCoreCheck) < 24*time.Hour {
			return update.Result{}, file.CoreWarning, false
		}
	}
	if !s.updateMu.TryLock() {
		return update.Result{}, "An update is already running.", true
	}
	defer s.updateMu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	result, err := s.Updater.Install(ctx, s.CoreDir)
	warning := ""
	if err != nil {
		warning = err.Error()
	}
	_ = settings.Update(s.SettingsPath, func(file *settings.File) {
		file.LastCoreCheck = time.Now()
		file.CoreWarning = warning
	})
	s.mu.Lock()
	s.Versions.UpdateWarning = warning
	if err == nil && result.Path != "" && s.Runner != nil {
		s.Runner.SetYtDlpPath(result.Path)
		if result.Version != "" {
			s.Versions.YtDlpVersion = result.Version
		}
	}
	s.mu.Unlock()
	return result, warning, true
}

func (s *Server) setAutoUpdate(ctx context.Context, msg protocol.Incoming) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(msg.Payload, &body)
	enabled := body.Enabled
	s.mu.Lock()
	s.AutoUpdate = enabled
	s.Versions.AutoUpdate = enabled
	s.mu.Unlock()
	_ = settings.Update(s.SettingsPath, func(file *settings.File) {
		file.AutoUpdate = &enabled
	})
	s.reply(msg, map[string]any{"enabled": enabled}, nil)
	if enabled {
		go s.publishCoreUpdate(ctx, false)
	}
}

func (s *Server) retryMux(msg protocol.Incoming) {
	var body struct {
		JobID     string `json:"jobId"`
		Container string `json:"container"`
	}
	_ = json.Unmarshal(msg.Payload, &body)
	if err := s.Jobs.RetryMux(body.JobID, body.Container); err != nil {
		s.reply(msg, nil, asAPI(err))
		return
	}
	s.reply(msg, map[string]any{"jobId": body.JobID}, nil)
}

func (s *Server) readInfo(ctx context.Context, page string) (media.Probe, error) {
	if s.Runner != nil {
		return s.Runner.Info(ctx, page)
	}
	return s.Info(ctx, page)
}

func (s *Server) version() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Versions.YtDlpVersion
}

func NewVersions(dir string, probed ytdlp.Versions) protocol.Handshake {
	return protocol.Handshake{
		ProtocolVersion: protocol.ProtocolVersion,
		HostVersion:     protocol.HostVersion,
		YtDlpVersion:    probed.YtDlp,
		FFmpegVersion:   probed.FFmpeg,
		Deno:            probed.HasDeno,
		DenoVersion:     probed.Deno,
		DownloadDir:     dir,
		AutoUpdate:      true,
	}
}
