package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"streamgrab/host/internal/failure"
	"streamgrab/host/internal/jobs"
	"streamgrab/host/internal/media"
	"streamgrab/host/internal/platform"
	"streamgrab/host/internal/protocol"
)

type Versions struct {
	YtDlp   string
	FFmpeg  string
	Deno    string
	HasDeno bool
}

type Runner struct {
	// YtDlpPath is set at construction. Use SetYtDlpPath once the runner is shared.
	YtDlpPath   string
	FFmpegPath  string
	DownloadDir string
	Prefix      []string
	mu          sync.RWMutex
}

// SetYtDlpPath switches to another yt-dlp, for example after an update.
func (r *Runner) SetYtDlpPath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.YtDlpPath = path
}

func (r *Runner) ytDlp() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.YtDlpPath
}

// Probe reads the tool versions. The tools are queried at the same time because
// each one can take seconds to start.
func (r *Runner) Probe(ctx context.Context) Versions {
	deno := DenoPath()
	versions := Versions{HasDeno: deno != ""}
	var wg sync.WaitGroup
	query := func(target *string, path string, args []string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			*target = commandVersion(ctx, path, args)
		}()
	}
	query(&versions.YtDlp, r.ytDlp(), r.Prefix)
	query(&versions.FFmpeg, r.FFmpegPath, nil)
	query(&versions.Deno, deno, []string{"--version"})
	wg.Wait()
	return versions
}

func (r *Runner) Info(ctx context.Context, page string) (media.Probe, error) {
	tool := r.ytDlp()
	if tool == "" {
		return media.Probe{}, &protocol.Error{Code: "EXTRACTION_FAILED", Message: "yt-dlp was not found on PATH."}
	}
	args := append(append([]string{}, r.Prefix...), "-J", "--no-playlist", "--no-warnings", "--encoding", "utf-8", page)
	cmd := exec.CommandContext(ctx, tool, args...)
	applyDenoEnv(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	tree, err := platform.StartTree(cmd)
	if err != nil {
		return media.Probe{}, classify(err.Error())
	}
	defer tree.Close()
	if err := cmd.Wait(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
		return media.Probe{}, classify(stderr.String() + "\n" + err.Error())
	}
	return decodeProbe(stdout.Bytes())
}

func (r *Runner) Start(ctx context.Context, job jobs.Job, onProgress func(media.Progress)) (jobs.Handle, error) {
	if job.RetryMux {
		return r.remux(ctx, job, onProgress)
	}
	tool := r.ytDlp()
	if tool == "" {
		return nil, &protocol.Error{Code: "EXTRACTION_FAILED", Message: "yt-dlp was not found on PATH."}
	}
	dir := r.outputDir(job)
	if job.Kind == "audio" {
		removeUnfinishedOutput(dir, job)
	}
	args, err := r.arguments(job)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(tool, args...)
	applyDenoEnv(cmd)

	var mu sync.Mutex
	var progress media.Progress
	if job.Kind == "audio" {
		progress.Stage = "downloading_audio"
	}
	var stderr tail
	read := func(line string) {
		mu.Lock()
		defer mu.Unlock()
		next := media.ParseLine(line, progress)
		changed := next.Stage != progress.Stage || next.BytesDone != progress.BytesDone || next.File != progress.File || next.OutputPath != progress.OutputPath || next.SawMerger != progress.SawMerger
		progress = next
		if changed && onProgress != nil {
			onProgress(next)
		}
	}
	return launch(ctx, cmd, read, func(line string) {
		stderr.add(line)
		read(line)
	}, func(waitErr error) (string, error) {
		mu.Lock()
		final := progress
		mu.Unlock()
		if waitErr != nil {
			return "", downloadError(stderr.String(), waitErr, final.SawMerger)
		}
		path := final.OutputPath
		if _, err := os.Stat(path); path == "" || err != nil {
			path = findOutput(dir, job.VideoID, job.Container)
		}
		return path, nil
	})
}

func downloadError(output string, waitErr error, sawMerger bool) error {
	if strings.TrimSpace(output) == "" {
		output = waitErr.Error()
	}
	result := failure.Classify(output)
	if sawMerger && result.Code != "DISK_FULL" && result.Code != "NETWORK_ERROR" {
		return &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept."}
	}
	return result
}

// removeUnfinishedOutput deletes a converted file that an earlier attempt of
// this job left behind. yt-dlp would otherwise accept it as complete.
func removeUnfinishedOutput(dir string, job jobs.Job) {
	for _, file := range job.Files {
		if filepath.Clean(filepath.Dir(file)) == filepath.Clean(dir) && strings.EqualFold(filepath.Ext(file), "."+job.Container) && !media.IsPartial(filepath.Base(file)) {
			_ = os.Remove(file)
		}
	}
}

// launch starts cmd and reports its output line by line. result turns the exit
// status into the finished file's path or an error.
func launch(ctx context.Context, cmd *exec.Cmd, stdout, stderr func(string), result func(error) (string, error)) (jobs.Handle, error) {
	runCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(runCtx, cmd.Path, cmd.Args[1:]...)
	command.Env = cmd.Env
	out := &lineWriter{emit: stdout}
	errOut := &lineWriter{emit: stderr}
	command.Stdout = out
	command.Stderr = errOut
	tree, err := platform.StartTree(command)
	if err != nil {
		cancel()
		return nil, err
	}
	handle := &execHandle{pid: command.Process.Pid, cancel: cancel, done: make(chan error, 1)}
	go func() {
		defer cancel()
		// Wait returns only after all output has been copied to the writers.
		waitErr := command.Wait()
		if errors.Is(waitErr, exec.ErrWaitDelay) {
			waitErr = nil
		}
		tree.Close()
		out.flush()
		errOut.flush()
		path, err := result(waitErr)
		handle.mu.Lock()
		handle.path = path
		handle.mu.Unlock()
		handle.done <- err
	}()
	return handle, nil
}

// lineWriter splits process output into lines.
type lineWriter struct {
	buf  []byte
	emit func(string)
}

const maxLine = 1 << 20

func (w *lineWriter) Write(data []byte) (int, error) {
	w.buf = append(w.buf, data...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(strings.TrimSuffix(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	if len(w.buf) > maxLine {
		w.flush()
	}
	return len(data), nil
}

func (w *lineWriter) flush() {
	if len(w.buf) > 0 {
		w.emit(strings.TrimSuffix(string(w.buf), "\r"))
		w.buf = nil
	}
}

// tail keeps the end of a process's error output.
type tail struct {
	mu   sync.Mutex
	text string
}

const maxTail = 32 << 10

func (t *tail) add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.text += line + "\n"
	if len(t.text) > maxTail {
		t.text = t.text[len(t.text)-maxTail:]
	}
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.text
}

type execHandle struct {
	pid    int
	cancel context.CancelFunc
	done   chan error
	mu     sync.Mutex
	path   string
}

func (h *execHandle) PID() int { return h.pid }

func (h *execHandle) Wait() error { return <-h.done }

// Kill stops the process and everything it started.
func (h *execHandle) Kill() error {
	h.cancel()
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
			Width          int     `json:"width"`
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
			Width:          format.Width,
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
	result := failure.Classify(text)
	switch {
	case result.Code == "NETWORK_ERROR":
		result.Message = "The network request failed."
	case strings.HasPrefix(result.Message, "The download failed."):
		reason := strings.TrimPrefix(result.Message, "The download failed. Update yt-dlp and try again.")
		result.Message = "StreamGrab couldn't read this video." + reason
	}
	return result
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

func (r *Runner) outputDir(job jobs.Job) string {
	if dir := strings.TrimSpace(job.Dir); dir != "" {
		return dir
	}
	return r.DownloadDir
}

func (r *Runner) arguments(job jobs.Job) ([]string, error) {
	dir := r.outputDir(job)
	if dir == "" {
		return nil, &protocol.Error{Code: "PATH_UNWRITABLE", Message: "Choose a download folder."}
	}
	ext := job.Container
	if ext == "" {
		ext = "mp4"
	}
	name := "%(title).180B [%(id)s]"
	// Extracting audio first downloads a source file with another extension, so
	// an audio job must avoid every existing file for the video.
	collides := ext
	if job.Kind == "audio" {
		collides = ""
	}
	if n := nextSuffix(dir, job.VideoID, collides, job.Files); n > 0 {
		name += fmt.Sprintf(" (%d)", n)
	}
	args := append([]string{}, r.Prefix...)
	args = append(args,
		"--no-playlist",
		"--newline",
		"--no-warnings",
		"--no-overwrites",
		"--encoding", "utf-8",
		// --print silences other output unless these are given.
		"--no-simulate", "--no-quiet", "--progress",
		"--print", "after_move:"+media.FilePrefix+"%(filepath)s",
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
		if holdsCover(container) {
			// YouTube serves WebP, which Windows can't show as a cover.
			args = append(args, "--embed-thumbnail", "--convert-thumbnails", "jpg")
		}
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

// holdsCover reports whether yt-dlp can embed a thumbnail in a video container.
// It fails the download when asked to do that for WebM.
func holdsCover(container string) bool {
	return container == "mp4" || container == "mkv"
}

// findCover returns the thumbnail that yt-dlp saved beside a downloaded stream
// and had not yet embedded, or "".
func findCover(video, formatID string) string {
	stem := strings.TrimSuffix(video, filepath.Ext(video))
	if !strings.HasSuffix(stem, ".f"+formatID) {
		return ""
	}
	path := strings.TrimSuffix(stem, ".f"+formatID) + ".jpg"
	if info, err := os.Stat(path); err != nil || info.IsDir() || info.Size() == 0 {
		return ""
	}
	return path
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
		if entry.IsDir() || media.IsPartial(entry.Name()) || !strings.Contains(entry.Name(), videoID) || !strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
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

// nextSuffix returns the number to add to a new file name so that it doesn't
// collide with an existing file. An empty ext matches every extension. Files in
// own belong to the job being started and are ignored.
func nextSuffix(dir, videoID, ext string, own []string) int {
	marker := "[" + videoID + "]"
	mine := map[string]bool{}
	for _, file := range own {
		mine[strings.ToLower(filepath.Base(file))] = true
	}
	max := -1
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || media.IsPartial(name) || mine[strings.ToLower(name)] || !strings.Contains(name, marker) {
			continue
		}
		suffix := filepath.Ext(name)
		if ext != "" && !strings.EqualFold(suffix, "."+ext) {
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

// findStreams locates the downloaded video and audio streams of a job.
func findStreams(dir string, job jobs.Job) (video, audio string) {
	return findStream(dir, job, job.VideoFormatID), findStream(dir, job, job.AudioFormatID)
}

// findStream returns the file holding one format of the job's video, preferring
// a file the job is known to have written.
func findStream(dir string, job jobs.Job, formatID string) string {
	if formatID == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	mine := map[string]bool{}
	for _, file := range job.Files {
		mine[filepath.Base(file)] = true
	}
	var match string
	var newest int64
	for _, entry := range entries {
		name := entry.Name()
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if entry.IsDir() || !strings.Contains(name, "["+job.VideoID+"]") || !strings.HasSuffix(stem, ".f"+formatID) {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(name)); ext == ".part" || ext == ".ytdl" {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Size() == 0 {
			continue
		}
		path := filepath.Join(dir, name)
		if mine[name] {
			return path
		}
		if info.ModTime().UnixNano() >= newest {
			newest, match = info.ModTime().UnixNano(), path
		}
	}
	return match
}

func (r *Runner) remux(ctx context.Context, job jobs.Job, onProgress func(media.Progress)) (jobs.Handle, error) {
	if r.FFmpegPath == "" {
		return nil, &protocol.Error{Code: "MUX_FAILED", Message: "FFmpeg was not found. The partial streams were kept."}
	}
	dir := r.outputDir(job)
	video, audio := findStreams(dir, job)
	if video == "" || (job.AudioFormatID != "" && audio == "") {
		return nil, &protocol.Error{Code: "MUX_FAILED", Message: "The partial streams are no longer in the download folder."}
	}
	container := job.Container
	if container == "" {
		container = "mp4"
	}
	out := uniquePath(dir, job.Title, job.VideoID, container, job.Files)
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(out))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, &protocol.Error{Code: "PATH_UNWRITABLE", Message: "The output path left the download folder."}
	}
	cover := ""
	if holdsCover(container) {
		cover = findCover(video, job.VideoFormatID)
	}
	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-i", video}
	inputs := 1
	audioMap := "0:a?"
	if audio != "" {
		args = append(args, "-i", audio)
		audioMap = "1:a:0"
		inputs++
	}
	// An MP4 cover is a second video stream. Matroska stores it as an attachment.
	coverStream := cover != "" && container == "mp4"
	if coverStream {
		args = append(args, "-i", cover)
	}
	if audio != "" || coverStream {
		args = append(args, "-map", "0:v:0", "-map", audioMap)
	}
	if coverStream {
		args = append(args, "-map", fmt.Sprintf("%d:v:0", inputs), "-disposition:v:1", "attached_pic")
	}
	if cover != "" && container == "mkv" {
		args = append(args, "-attach", cover, "-metadata:s:t", "mimetype=image/jpeg", "-metadata:s:t", "filename=cover.jpg")
	}
	args = append(args, "-c", "copy")
	if job.RecodeAudio {
		switch container {
		case "mp4":
			args = append(args, "-c:a", "aac", "-b:a", "192k")
		case "webm":
			args = append(args, "-c:a", "libopus", "-b:a", "160k")
		}
	}
	if container == "mp4" {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, out)
	if onProgress != nil {
		onProgress(media.Progress{Stage: "muxing", Percent: 50, File: out})
	}
	var output tail
	return launch(ctx, exec.Command(r.FFmpegPath, args...), output.add, output.add, func(waitErr error) (string, error) {
		if waitErr != nil {
			_ = os.Remove(out)
			detail := strings.TrimSpace(output.String())
			if detail == "" {
				detail = waitErr.Error()
			}
			return "", &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept. " + failure.Truncate(detail, 180)}
		}
		info, statErr := os.Stat(out)
		if statErr != nil || info.Size() == 0 {
			_ = os.Remove(out)
			return "", &protocol.Error{Code: "MUX_FAILED", Message: "Muxing failed. The partial streams were kept."}
		}
		if cover != "" {
			_ = os.Remove(cover)
		}
		return out, nil
	})
}

func uniquePath(dir, title, videoID, ext string, own []string) string {
	base := safeTitle(title, videoID)
	suffix := ""
	if n := nextSuffix(dir, videoID, ext, own); n > 0 {
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
	clean = strings.TrimSpace(failure.Truncate(clean, 80))
	if clean == "" {
		return id
	}
	return clean
}

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
