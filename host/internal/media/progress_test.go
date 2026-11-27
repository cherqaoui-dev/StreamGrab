package media

import "testing"

func TestParseProgressTemplate(t *testing.T) {
	got := ParseLine("streamgrab:400:1000:0:250000:8", Progress{})
	if got.Stage != "downloading_video" || got.BytesDone != 400 || got.BytesTotal != 1000 {
		t.Fatalf("%+v", got)
	}
	if got.Percent < 39 || got.Percent > 41 || got.EtaSec == nil || *got.EtaSec != 8 {
		t.Fatalf("%+v", got)
	}
	audio := ParseLine("[download] Destination: clip.f140.m4a", Progress{Stage: "downloading_video", Percent: 100})
	if audio.Stage != "downloading_audio" {
		t.Fatalf("stage = %s", audio.Stage)
	}
	merged := ParseLine(`[Merger] Merging formats into "C:\Videos\clip.mp4"`, audio)
	if merged.Stage != "muxing" || merged.OutputPath != `C:\Videos\clip.mp4` {
		t.Fatalf("%+v", merged)
	}
}

func TestIgnoresNA(t *testing.T) {
	got := ParseLine("streamgrab:NA:NA:NA:NA:NA", Progress{Stage: "downloading_video"})
	if got.BytesDone != 0 || got.EtaSec != nil {
		t.Fatalf("%+v", got)
	}
}

func TestParseLineRecordsThumbnails(t *testing.T) {
	for line, want := range map[string]string{
		`[info] Writing video thumbnail 41 to: C:\v\A [id].webp`:                  `C:\v\A [id].webp`,
		`[youtube] id: Writing thumbnail to: C:\v\A [id].jpg`:                     `C:\v\A [id].jpg`,
		`[ThumbnailsConvertor] Converting thumbnail "C:\v\A [id].webp" to jpg`:    `C:\v\A [id].jpg`,
		`[info] Writing video metadata as JSON to: C:\v\thumbnail [id].info.json`: "",
		`[download] Destination: C:\v\Writing thumbnail to: x [id].f137.mp4`:      `C:\v\Writing thumbnail to: x [id].f137.mp4`,
	} {
		if got := ParseLine(line, Progress{}).File; got != want {
			t.Errorf("%s: file = %q, want %q", line, got, want)
		}
	}
}
