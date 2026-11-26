package ytdlp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"streamgrab/host/internal/jobs"
	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
)

type Versions struct {
	YtDlp   string
	FFmpeg  string
	Deno    string
	HasDeno bool
}

type Runner struct {
	YtDlpPath   string
	FFmpegPath  string
	DownloadDir string
	Dir         *string
	Prefix      []string
}

func (r Runner) Probe(ctx context.Context) Versions {
	deno := DenoPath()
	return Versions{
		YtDlp:   commandVersion(ctx, r.YtDlpPath, r.Prefix),
		FFmpeg:  commandVersion(ctx, r.FFmpegPath, nil),
		Deno:    commandVersion(ctx, deno, []string{"--version"}),
		HasDeno: deno != "",
	}
}

func (r Runner) Info(ctx context.Context, page string) (media.Probe, error) {
	if r.YtDlpPath == "" {
		return media.Probe{}, &protocol.Error{Code: "EXTRACTION_FAILED", Message: "yt-dlp was not found on PATH."}
	}
	cmd := exec.CommandContext(ctx, r.YtDlpPath, append(r.Prefix, "-J", "--no-playlist", "--no-warnings", page)...)
	applyDenoEnv(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return media.Probe{}, classify(stderr.String() + err.Error())
	}
	return decodeProbe(stdout.Bytes())
}

func (r Runner) Start(ctx context.Context, job jobs.Job, onProgress func(media.Progress)) (jobs.Handle, error) {
	if job.RetryMux {
		return r.remux(ctx, job, onProgress)
	}
	if r.YtDlpPath == "" {
		return nil, &protocol.Error{Code: "EXTRACTION_FAILED", Message: "yt-dlp was not found on PATH."}
	}
	args, err := r.arguments(job)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(runCtx, r.YtDlpPath, args...)
	applyDenoEnv(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	handle := &execHandle{cmd: cmd, cancel: cancel, done: make(chan error, 1)}
	go func() {
		var progress media.Progress
		if job.Kind == "audio" {
			progress.Stage = "downloading_audio"
		}
		var progressMu sync.Mutex
		var stderrBuf strings.Builder
		var stderrMu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(2)
		scan := func(reader io.Reader, errOut bool) {
			defer wg.Done()
			scanner := bufio.NewScanner(reader)
			scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if errOut {
					stderrMu.Lock()
					stderrBuf.WriteString(line)
					stderrBuf.WriteByte('\n')
					stderrMu.Unlock()
				}
				progressMu.Lock()
				next := media.ParseLine(line, progress)
				changed := next.Stage != progress.Stage || next.BytesDone != progress.BytesDone || next.OutputPath != progress.OutputPath || next.SawMerger != progress.SawMerger
				progress = next
				progressMu.Unlock()
				if changed && onProgress != nil {
					onProgress(next)
				}
			}
		}
		go scan(stdout, false)
		go scan(stderr, true)
		waitErr := cmd.Wait()
		wg.Wait()
		stderrMu.Lock()
		text := stderrBuf.String()
		stderrMu.Unlock()
		progressMu.Lock()
		final := progress
		progressMu.Unlock()
		handle.mu.Lock()
		handle.path = final.OutputPath
		if handle.path == "" {
			handle.path = findOutput(r.outputDir(), job.VideoID, job.Container)
		}
		handle.mu.Unlock()
		if waitErr != nil {
			if final.SawMerger {
				waitErr = errors.New("mux " + text)
			} else if text != "" {
				waitErr = errors.New(text)
			}
		}
		handle.done <- waitErr
	}()
	return handle, nil
}

type execHandle struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan error
	mu     sync.Mutex
	path   string
}

func (h *execHandle) PID() int {
	if h.cmd.Process == nil {
		return 0
	}
	return h.cmd.Process.Pid
}

func (h *execHandle) Wait() error { return <-h.done }

func (h *execHandle) Kill() error {
	h.cancel()
	if h.cmd.Process != nil {
		_ = h.cmd.Process.Kill()
	}
	return nil
}

func (h *execHandle) OutputPath() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.path
}

func decodeProbe(raw []byte) (media.Probe, error) {
	var body struct {
		ID         string  `json:"id"`
		Title      string  `json:"title"`
		Channel    string  `json:"channel"`
		Uploader   string  `json:"uploader"`
		Thumbnail  string  `json:"thumbnail"`
		Duration   float64 `json:"duration"`
		ViewCount  float64 `json:"view_count"`
		IsLive     bool    `json:"is_live"`
		LiveStatus string  `json:"live_status"`
		AgeLimit   int     `json:"age_limit"`
		Formats    []struct {
			FormatID       string  `json:"format_id"`
			Height         int     `json:"height"`
			FPS            float64 `json:"fps"`
			VCodec         string  `json:"vcodec"`
			ACodec         string  `json:"acodec"`
			Ext            string  `json:"ext"`
			Filesize       float64 `json:"filesize"`
			FilesizeApprox float64 `json:"filesize_approx"`
			TBR            float64 `json:"tbr"`
			ABR            float64 `json:"abr"`
			ASR            float64 `json:"asr"`
			DynamicRange   string  `json:"dynamic_range"`
			URL            string  `json:"url"`
			HasDRM         bool    `json:"has_drm"`
		} `json:"formats"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return media.Probe{}, &protocol.Error{Code: "EXTRACTION_FAILED", Message: "StreamGrab couldn't read this video."}
	}
	probe := media.Probe{
		ID:        body.ID,
		Title:     body.Title,
		Channel:   body.Channel,
		Thumbnail: body.Thumbnail,
		Duration:  body.Duration,
		ViewCount: int64(body.ViewCount),
		IsLive:    body.IsLive || body.LiveStatus == "is_live",
		AgeLimit:  body.AgeLimit,
	}
	if probe.Channel == "" {
		probe.Channel = body.Uploader
	}
	for _, format := range body.Formats {
		probe.Formats = append(probe.Formats, media.Format{
			FormatID:       format.FormatID,
			Height:         format.Height,
			FPS:            format.FPS,
			VCodec:         format.VCodec,
			ACodec:         format.ACodec,
			Ext:            format.Ext,
			Filesize:       format.Filesize,
			FilesizeApprox: format.FilesizeApprox,
			TBR:            format.TBR,
			ABR:            format.ABR,
			ASR:            int(format.ASR),
			DynamicRange:   format.DynamicRange,
			URL:            format.URL,
			HasDRM:         format.HasDRM,
		})
	}
	return probe, nil
}

func classify(text string) error {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "drm"):
		return &protocol.Error{Code: "DRM_PROTECTED", Message: "StreamGrab doesn't support DRM content."}
	case strings.Contains(lower, "confirm your age"), strings.Contains(lower, "age-restricted"), strings.Contains(lower, "sign in to confirm"):
		return &protocol.Error{Code: "AGE_RESTRICTED", Message: "This video is age-restricted. Browser cookies are not enabled in this version."}
	case strings.Contains(lower, "live event"), strings.Contains(lower, "is live"):
		return &protocol.Error{Code: "LIVE_STREAM", Message: "This live stream can't be downloaded in this version."}
	case strings.Contains(lower, "javascript runtime"), strings.Contains(lower, "challenge solving failed"), strings.Contains(lower, "signature solving failed"):
		return &protocol.Error{Code: "EXTRACTION_FAILED", Message: "StreamGrab couldn't read this video because Deno was not available to yt-dlp."}
	case strings.Contains(lower, "network"), strings.Contains(lower, "timed out"):
		return &protocol.Error{Code: "NETWORK_ERROR", Message: "The network request failed."}
	default:
		message := "StreamGrab couldn't read this video."
		if reason := clipReason(text); reason != "" {
			message += " " + reason
		}
		return &protocol.Error{Code: "EXTRACTION_FAILED", Message: message}
	}
}

func clipReason(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "ERROR:"))
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "warning:") || strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
			continue
		}
		if len(line) > 160 {
			line = line[:160]
		}
		return line
	}
	return ""
}

func CommandVersion(ctx context.Context, path string) string {
	return commandVersion(ctx, path, nil)
}

func commandVersion(ctx context.Context, path string, prefix []string) string {
	if path == "" {
		return ""
	}
	args := append([]string{}, prefix...)
	if len(prefix) == 0 {
		args = []string{"--version"}
	}
	cmd := exec.CommandContext(ctx, path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if strings.HasPrefix(strings.ToLower(line), "ffmpeg version ") {
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			return parts[2]
		}
	}
	return line
}

func look(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (r Runner) outputDir() string {
	if r.Dir != nil && strings.TrimSpace(*r.Dir) != "" {
		return strings.TrimSpace(*r.Dir)
	}
	return r.DownloadDir
}

func (r Runner) arguments(job jobs.Job) ([]string, error) {
	dir := r.outputDir()
	if dir == "" {
		return nil, &protocol.Error{Code: "PATH_UNWRITABLE", Message: "Choose a download folder."}
	}
	ext := job.Container
	if ext == "" {
		ext = "mp4"
	}
	name := "%(title).180B [%(id)s]"
	if n := nextSuffix(dir, job.VideoID, ext); n > 0 {
		name += fmt.Sprintf(" (%d)", n)
	}
	args := append([]string{}, r.Prefix...)
	args = append(args,
		"--no-playlist",
		"--newline",
		"--no-warnings",
		"--no-overwrites",
		"--progress-template", "streamgrab:%(progress.downloaded_bytes)s:%(progress.total_bytes)s:%(progress.total_bytes_estimate)s:%(progress.speed)s:%(progress.eta)s",
		"-o", filepath.Join(dir, name+".%(ext)s"),
		"--paths", "home:"+dir,
	)
	if job.Kind == "audio" {
		args = append(args, audioArgs(job)...)
	} else {
		format := job.VideoFormatID
		if job.AudioFormatID != "" {
			format += "+" + job.AudioFormatID
		}
		container := job.Container
		if container == "" {
			container = "mp4"
		}
		args = append(args, "--merge-output-format", container, "-f", format)
		args = append(args, recodeArgs(job)...)
	}
	if runtime.GOOS == "windows" {
		args = append(args, "--windows-filenames")
	}
	if r.FFmpegPath != "" {
		args = append(args, "--ffmpeg-location", filepath.Dir(r.FFmpegPath))
	}
	if job.ResumeDownload {
		args = append(args, "--continue")
	}
	args = append(args, job.URL)
	return args, nil
}

func audioArgs(job jobs.Job) []string {
	args := []string{
		"-f", job.AudioFormatID,
		"-x",
		"--audio-format", job.Container,
		"--embed-metadata",
		"--embed-thumbnail",
		"--convert-thumbnails", "jpg",
		"--parse-metadata", "%(title)s:%(meta_title)s",
		"--parse-metadata", "%(channel,creator,uploader)s:%(meta_artist)s",
		"--parse-metadata", "%(channel,creator,uploader)s:%(meta_album)s",
		"--parse-metadata", "%(upload_date.4)s:%(meta_date)s",
		"--parse-metadata", "%(webpage_url)s:%(meta_comment)s",
	}
	switch job.Container {
	case "mp3":
		args = append(args, "--audio-quality", "320K")
	case "m4a":
		if job.RecodeAudio {
			args = append(args, "--audio-quality", "256K")
		}
	case "opus":
		if job.RecodeAudio {
			args = append(args, "--audio-quality", "160K")
		}
	case "flac":
		rate := job.SampleRate
		if rate <= 0 || rate > 48000 {
			rate = 48000
		}
		args = append(args, "--postprocessor-args", fmt.Sprintf("ExtractAudio:-sample_fmt s32 -ar %d", rate))
	}
	return args
}

func recodeArgs(job jobs.Job) []string {
	if !job.RecodeAudio {
		return nil
	}
	switch job.Container {
	case "", "mp4":
		return []string{"--postprocessor-args", "Merger:-c:v copy -c:a aac -b:a 192k"}
	case "webm":
		return []string{"--postprocessor-args", "Merger:-c:v copy -c:a libopus -b:a 160k"}
	default:
		return nil
	}
}

func findOutput(dir, videoID, ext string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var match string
	var newest int64
	for _, entry := range entries {
		suffix := ".mp4"
		if ext != "" {
			suffix = "." + strings.ToLower(ext)
		}
		if entry.IsDir() || isStreamFragment(entry.Name()) || !strings.Contains(entry.Name(), videoID) || !strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().UnixNano() >= newest {
			newest = info.ModTime().UnixNano()
			match = filepath.Join(dir, entry.Name())
		}
	}
	return match
}

var fragmentTail = regexp.MustCompile(`\.f[0-9]{2,4}\.[a-z0-9]+$`)

func nextSuffix(dir, videoID, ext string) int {
	marker := "[" + videoID + "]"
	suffix := "." + strings.ToLower(ext)
	max := -1
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || isStreamFragment(name) || !strings.Contains(name, marker) || !strings.HasSuffix(strings.ToLower(name), suffix) {
			continue
		}
		if n := collisionNumber(name, marker, suffix); n > max {
			max = n
		}
	}
	if max < 0 {
		return 0
	}
	return max + 1
}

func collisionNumber(name, marker, ext string) int {
	index := strings.LastIndex(name, marker)
	if index < 0 {
		return 0
	}
	rest := name[index+len(marker):]
	if len(rest) < len(ext) || !strings.EqualFold(rest[len(rest)-len(ext):], ext) {
		return 0
	}
	rest = strings.TrimSpace(rest[:len(rest)-len(ext)])
	if rest == "" {
		return 0
	}
	if len(rest) < 3 || rest[0] != '(' || rest[len(rest)-1] != ')' {
		return 0
	}
	n, err := strconv.Atoi(rest[1 : len(rest)-1])
	if err != nil || n < 1 {
		return 0
	}
	return n
}

func isStreamFragment(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".ytdl") {
		return true
	}
	return fragmentTail.MatchString(lower)
}

func findStreams(dir, videoID string) (string, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	var video, audio string
	var videoSize, audioSize int64
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.Contains(name, videoID) || !fragmentTail.MatchString(strings.ToLower(name)) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(dir, name)
		if isAudioFragment(name) {
			if info.Size() >= audioSize {
				audio, audioSize = path, info.Size()
			}
			continue
		}
		if info.Size() >= videoSize {
			video, videoSize = path, info.Size()
		}
	}
	return video, audio
}

func isAudioFragment(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".m4a"), strings.HasSuffix(lower, ".opus"), strings.HasSuffix(lower, ".ogg"), strings.HasSuffix(lower, ".aac"), strings.HasSuffix(lower, ".mp3"), strings.HasSuffix(lower, ".flac"):
		return true
	default:
		return strings.Contains(lower, ".f249.") || strings.Contains(lower, ".f250.") || strings.Contains(lower, ".f251.")
	}
}

func (r Runner) remux(ctx context.Context, job jobs.Job, onProgress func(media.Progress)) (jobs.Handle, error) {
	if r.FFmpegPath == "" {
		return nil, &protocol.Error{Code: "MUX_FAILED", Message: "FFmpeg was not found. The partial streams were kept."}
	}
	dir := r.outputDir()
	video, audio := findStreams(dir, job.VideoID)
	if video == "" && audio == "" {
		return nil, &protocol.Error{Code: "MUX_FAILED", Message: "The partial streams are no longer in the download folder."}
	}
	container := job.Container
	if container == "" {
		container = "mp4"
	}
	out := uniquePath(dir, job.Title, job.VideoID, container)
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(out))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, &protocol.Error{Code: "PATH_UNWRITABLE", Message: "The output path left the download folder."}
	}
	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	if video != "" && audio != "" {
		args = append(args, "-i", video, "-i", audio, "-map", "0:v:0", "-map", "1:a:0", "-c", "copy")
	} else {
		source := video
		if source == "" {
			source = audio
		}
		args = append(args, "-i", source, "-c", "copy")
	}
	if container == "mp4" {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, out)
	if onProgress != nil {
		onProgress(media.Progress{Stage: "muxing", Percent: 50})
	}
	cmd := exec.CommandContext(ctx, r.FFmpegPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(out)
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		if len(detail) > 180 {
			detail = detail[:180]
		}
		return nil, &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept. " + detail}
	}
	info, statErr := os.Stat(out)
	if statErr != nil || info.Size() == 0 {
		_ = os.Remove(out)
		return nil, &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept."}
	}
	if onProgress != nil {
		onProgress(media.Progress{Stage: "muxing", Percent: 100, OutputPath: out, SawMerger: true})
	}
	return doneHandle{path: out}, nil
}

func uniquePath(dir, title, videoID, ext string) string {
	base := safeTitle(title, videoID)
	suffix := ""
	if n := nextSuffix(dir, videoID, ext); n > 0 {
		suffix = fmt.Sprintf(" (%d)", n)
	}
	return filepath.Join(dir, fmt.Sprintf("%s [%s]%s.%s", base, videoID, suffix, ext))
}

func safeTitle(title, id string) string {
	clean := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, title)
	clean = strings.Trim(strings.TrimSpace(clean), ". ")
	if len(clean) > 80 {
		clean = strings.TrimSpace(clean[:80])
	}
	if clean == "" {
		return id
	}
	return clean
}

type doneHandle struct {
	path string
}

func (h doneHandle) PID() int           { return 0 }
func (h doneHandle) Wait() error        { return nil }
func (h doneHandle) Kill() error        { return nil }
func (h doneHandle) OutputPath() string { return h.path }

func PreferredBinary(dir string) string {
	if path := managedBinary(dir); path != "" {
		return path
	}
	return ResolveTool("yt-dlp")
}

func managedBinary(dir string) string {
	if strings.TrimSpace(dir) == "" {
		return ""
	}
	name := "yt-dlp"
	if runtime.GOOS == "windows" {
		name = "yt-dlp.exe"
	}
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}
	return path
}

func ResolveTool(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}

func DenoPath() string {
	if path := ResolveTool("deno"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	name := "deno"
	if runtime.GOOS == "windows" {
		name = "deno.exe"
	}
	candidate := filepath.Join(home, ".deno", "bin", name)
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}

func applyDenoEnv(cmd *exec.Cmd) {
	deno := DenoPath()
	if deno == "" {
		return
	}
	cmd.Env = prependPath(os.Environ(), filepath.Dir(deno))
}

func prependPath(env []string, dir string) []string {
	if dir == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	found := false
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(name, "Path") {
			out = append(out, name+"="+dir+string(os.PathListSeparator)+value)
			found = true
			continue
		}
		out = append(out, entry)
	}
	if !found {
		out = append(out, "PATH="+dir)
	}
	return out
}

func MustDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create download directory: %w", err)
	}
	return nil
}
