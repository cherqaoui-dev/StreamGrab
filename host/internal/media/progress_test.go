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
