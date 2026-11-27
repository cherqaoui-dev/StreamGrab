//go:build !windows

package ytdlp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"streamgrab/host/internal/jobs"
	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
)

// fakeTool writes a shell script that stands in for yt-dlp or FFmpeg.
func fakeTool(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStartReportsConvertedAudioPath(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "Song [abcdefghijk].mp3")
	if err := os.WriteFile(final, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "Song [abcdefghijk].webm")
	runner := &Runner{DownloadDir: dir, YtDlpPath: fakeTool(t, `
echo "[download] Destination: `+source+`"
echo "[ExtractAudio] Destination: `+final+`"
printf 'streamgrab-file:%s' "`+final+`"`)}
	var files []string
	handle, err := runner.Start(context.Background(), jobs.Job{Kind: "audio", Container: "mp3", AudioFormatID: "251", VideoID: "abcdefghijk"}, func(p media.Progress) {
		files = append(files, p.File)
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(); err != nil {
		t.Fatal(err)
	}
	if handle.OutputPath() != final {
		t.Fatalf("path = %q", handle.OutputPath())
	}
	if !strings.Contains(strings.Join(files, "|"), source) {
		t.Fatalf("files = %q", files)
	}
}

func TestStartClassifiesFailure(t *testing.T) {
	runner := &Runner{DownloadDir: t.TempDir(), YtDlpPath: fakeTool(t, `echo "ERROR: [Errno 104] Connection reset by peer" >&2; exit 1`)}
	handle, err := runner.Start(context.Background(), jobs.Job{VideoID: "abcdefghijk"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	api, ok := handle.Wait().(*protocol.Error)
	if !ok || api.Code != "NETWORK_ERROR" {
		t.Fatalf("%+v", api)
	}
}

func TestKillStopsChildProcesses(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child")
	runner := &Runner{DownloadDir: t.TempDir(), YtDlpPath: fakeTool(t, `sleep 60 & echo $! > `+pidFile+`; wait`)}
	handle, err := runner.Start(context.Background(), jobs.Job{VideoID: "abcdefghijk"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var child int
	for deadline := time.Now().Add(5 * time.Second); child == 0 && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		raw, _ := os.ReadFile(pidFile)
		child, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	if child == 0 {
		t.Fatal("child did not start")
	}
	_ = handle.Kill()
	done := make(chan struct{})
	go func() { _ = handle.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit")
	}
	for deadline := time.Now().Add(5 * time.Second); alive(child); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("child process survived")
		}
	}
}

func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	// A killed process can linger as a zombie until it is reaped.
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	return err != nil || !strings.Contains(string(raw), ") Z ")
}

func TestRemuxUsesTheJobsOwnStreams(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"T [abcdefghijk].f136.mp4", "T [abcdefghijk].f137.mp4", "T [abcdefghijk].f251-drc.webm"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	job := jobs.Job{VideoID: "abcdefghijk", Title: "T", Container: "mkv", VideoFormatID: "136", AudioFormatID: "251-drc", RetryMux: true}
	video, audio := findStreams(dir, job)
	if !strings.HasSuffix(video, ".f136.mp4") || !strings.HasSuffix(audio, ".f251-drc.webm") {
		t.Fatalf("%q %q", video, audio)
	}
	runner := &Runner{DownloadDir: dir, FFmpegPath: fakeTool(t, `for last; do :; done; echo data > "$last"`)}
	handle, err := runner.Start(context.Background(), job, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Wait(); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "T [abcdefghijk].mkv"); handle.OutputPath() != want {
		t.Fatalf("path = %q", handle.OutputPath())
	}
	job.AudioFormatID = "999"
	if _, err := runner.Start(context.Background(), job, nil); err == nil {
		t.Fatal("remux without the audio stream should fail")
	}
}

func TestAudioNameAvoidsFinishedVideo(t *testing.T) {
	dir := t.TempDir()
	video := filepath.Join(dir, "T [abcdefghijk].webm")
	if err := os.WriteFile(video, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := nextSuffix(dir, "abcdefghijk", "", nil); n != 1 {
		t.Fatalf("suffix = %d", n)
	}
	if n := nextSuffix(dir, "abcdefghijk", "", []string{video}); n != 0 {
		t.Fatalf("own file counted: %d", n)
	}
}
