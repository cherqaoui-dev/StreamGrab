package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"streamgrab/host/internal/jobs"
)

func TestDecodeProbe(t *testing.T) {
	raw := []byte(`{
		"id": "abcdefghijk",
		"title": "Example",
		"uploader": "Channel",
		"duration": 12,
		"is_live": false,
		"formats": [
			{"format_id": "136", "height": 720, "fps": 30, "vcodec": "avc1", "acodec": "none", "filesize": 1000}
		]
	}`)
	probe, err := decodeProbe(raw)
	if err != nil {
		t.Fatal(err)
	}
	if probe.ID != "abcdefghijk" || probe.Channel != "Channel" || len(probe.Formats) != 1 {
		t.Fatalf("%+v", probe)
	}
}

func TestAudioArgumentsEmbedTags(t *testing.T) {
	runner := Runner{DownloadDir: `C:\Videos`, FFmpegPath: `C:\ffmpeg\bin\ffmpeg.exe`}
	args, err := runner.arguments(jobs.Job{
		Kind: "audio", Container: "mp3", AudioFormatID: "251", URL: "https://www.youtube.com/watch?v=abcdefghijk", RecodeAudio: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	for _, want := range []string{"--audio-format\nmp3", "--audio-quality\n320K", "--embed-metadata", "--embed-thumbnail", "meta_artist", "meta_comment"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %s", want, joined)
		}
	}
	if strings.Contains(joined, "--merge-output-format") {
		t.Fatal("audio jobs should not merge a video container")
	}
}

func TestVideoContainerArgument(t *testing.T) {
	runner := Runner{DownloadDir: `C:\Videos`}
	args, err := runner.arguments(jobs.Job{
		Container: "webm", VideoFormatID: "248", AudioFormatID: "251", URL: "https://www.youtube.com/watch?v=abcdefghijk", RecodeAudio: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "--merge-output-format\nwebm") || !strings.Contains(joined, "libopus") {
		t.Fatalf("args = %s", joined)
	}
}

func TestNextSuffixSkipsFragments(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"Clip [abcdefghijk].f137.mp4",
		"Clip [abcdefghijk].mp4",
		"Clip [abcdefghijk] (1).mp4",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if n := nextSuffix(dir, "abcdefghijk", "mp4"); n != 2 {
		t.Fatalf("suffix = %d", n)
	}
	video, audio := findStreams(dir, "abcdefghijk")
	if video == "" || audio != "" {
		t.Fatalf("streams video=%s audio=%s", video, audio)
	}
}

func TestClassifyAgeRestricted(t *testing.T) {
	err := classify("ERROR: Sign in to confirm your age")
	if err == nil || !strings.Contains(err.Error(), "age-restricted") {
		t.Fatalf("%v", err)
	}
}

func TestClassifyMissingRuntime(t *testing.T) {
	err := classify("ERROR: n challenge solving failed. No supported JavaScript runtime could be found.")
	if err == nil || !strings.Contains(err.Error(), "Deno") {
		t.Fatalf("%v", err)
	}
}

func TestPreferredBinaryUsesManagedCopy(t *testing.T) {
	dir := t.TempDir()
	name := "yt-dlp"
	if os.PathSeparator == '\\' {
		name = "yt-dlp.exe"
	}
	managed := filepath.Join(dir, name)
	if err := os.WriteFile(managed, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PreferredBinary(dir); got != managed {
		t.Fatalf("preferred %s", got)
	}
	if got := PreferredBinary(t.TempDir()); got == managed {
		t.Fatal("empty directory should not reuse another copy")
	}
}

func TestPrependPath(t *testing.T) {
	got := prependPath([]string{"Path=C:\\Windows", "FOO=1"}, `C:\deno`)
	want := "Path=" + `C:\deno` + string(os.PathListSeparator) + `C:\Windows`
	if len(got) != 2 || got[0] != want || got[1] != "FOO=1" {
		t.Fatalf("%q", got)
	}
}
