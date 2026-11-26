package media

import "testing"

func TestBuildRowsAndFastest(t *testing.T) {
	probe := Probe{
		ID:        "abcdefghijk",
		Duration:  60,
		ViewCount: 1200000,
		Formats: []Format{
			{FormatID: "337", Height: 2160, FPS: 60, VCodec: "av01.0.12M.08", ACodec: "none", Filesize: 1_420_000_000, DynamicRange: "HDR10"},
			{FormatID: "271", Height: 1440, FPS: 60, VCodec: "vp9", ACodec: "none", FilesizeApprox: 700_000_000},
			{FormatID: "299", Height: 1080, FPS: 60, VCodec: "avc1.640028", ACodec: "none", Filesize: 300_000_000},
			{FormatID: "136", Height: 720, FPS: 30, VCodec: "avc1.4d401f", ACodec: "none", Filesize: 120_000_000},
			{FormatID: "251", ACodec: "opus", ABR: 160, Filesize: 40_000_000},
			{FormatID: "140", ACodec: "mp4a.40.2", ABR: 128, Filesize: 20_000_000},
		},
	}
	got := Build(probe)
	if len(got.Rows) != 4 {
		t.Fatalf("rows = %d", len(got.Rows))
	}
	if got.Rows[0].Label != "2160p 4K UHD" || !got.Rows[0].HDR || got.Rows[0].VideoCodec != "AV1" {
		t.Fatalf("top row = %+v", got.Rows[0])
	}
	if got.Rows[0].AudioFormatID != "140" || got.Rows[0].AudioCodec != "AAC" {
		t.Fatalf("expected AAC pair, got %+v", got.Rows[0])
	}
	if !got.Rows[0].Exact {
		t.Fatal("exact sizes should stay exact")
	}
	fastest := 0
	for _, row := range got.Rows {
		if row.Fastest {
			fastest++
			if row.Height != 720 {
				t.Fatalf("fastest height = %d", row.Height)
			}
		}
	}
	if fastest != 1 {
		t.Fatalf("fastest count = %d", fastest)
	}
	if len(got.Chips) == 0 || got.Chips[0] != "4K 60FPS" {
		t.Fatalf("chips = %#v", got.Chips)
	}
	joined := stringsJoin(got.Chips)
	if !contains(joined, "HDR") || !contains(joined, "AV1") || !contains(joined, "Opus 160k") {
		t.Fatalf("chips = %#v", got.Chips)
	}
}

func TestHidesMissingTiers(t *testing.T) {
	got := Build(Probe{Formats: []Format{
		{FormatID: "18", Height: 360, FPS: 30, VCodec: "avc1", ACodec: "mp4a.40.2", Filesize: 1000},
		{FormatID: "136", Height: 720, FPS: 30, VCodec: "avc1", ACodec: "none", TBR: 800},
	}, Duration: 10})
	if len(got.Rows) != 1 || got.Rows[0].Height != 720 {
		t.Fatalf("rows = %+v", got.Rows)
	}
	if got.Rows[0].Exact {
		t.Fatal("tbr estimate should be marked inexact")
	}
}

func TestContainerAndAudioTags(t *testing.T) {
	probe := Probe{
		Duration: 60,
		Formats: []Format{
			{FormatID: "337", Height: 2160, FPS: 60, VCodec: "av01", ACodec: "none", Filesize: 1_420_000_000},
			{FormatID: "399", Height: 1080, FPS: 30, VCodec: "av01", ACodec: "none", Filesize: 400_000_000},
			{FormatID: "299", Height: 1080, FPS: 30, VCodec: "avc1", ACodec: "none", Filesize: 300_000_000},
			{FormatID: "136", Height: 720, FPS: 30, VCodec: "avc1", ACodec: "none", Filesize: 120_000_000},
			{FormatID: "251", ACodec: "opus", ABR: 160, Filesize: 40_000_000},
			{FormatID: "140", ACodec: "mp4a.40.2", ABR: 128, Filesize: 20_000_000},
		},
	}
	got := Build(probe)
	webm := got.Presets["av1-vp9-h264"]["webm"]
	for _, row := range webm {
		if row.Height == 720 && (row.Available || row.Reason == "") {
			t.Fatalf("720p webm = %+v", row)
		}
	}
	var h264Row Row
	for _, row := range got.Presets["h264-vp9-av1"]["mp4"] {
		if row.Height == 1080 {
			h264Row = row
		}
	}
	if h264Row.VideoCodec != "H.264" || h264Row.VideoFormatID != "299" {
		t.Fatalf("h264 preference = %+v", h264Row)
	}
	var direct, best string
	for _, row := range got.Audio {
		if row.Tag == "Direct" {
			direct = row.ID
		}
		if row.Tag == "Best" {
			best = row.ID
		}
		if row.ID == "m4a" && (!row.Transcoded || row.Copy) {
			t.Fatalf("m4a = %+v", row)
		}
		if row.ID == "flac" && row.FormatID != "251" {
			t.Fatalf("flac source = %+v", row)
		}
	}
	if direct != "opus" || best != "mp3" {
		t.Fatalf("tags direct=%s best=%s", direct, best)
	}
}

func TestNoHDRChipWithoutHDR(t *testing.T) {
	got := Build(Probe{Formats: []Format{
		{FormatID: "136", Height: 720, FPS: 30, VCodec: "avc1", ACodec: "mp4a.40.2", DynamicRange: "SDR", Filesize: 10},
	}})
	for _, chip := range got.Chips {
		if chip == "HDR" {
			t.Fatal("unexpected HDR chip")
		}
	}
}

func contains(s, part string) bool {
	return len(s) >= len(part) && (s == part || len(part) == 0 || indexOf(s, part) >= 0)
}

func indexOf(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

func stringsJoin(parts []string) string {
	out := ""
	for _, part := range parts {
		out += part + " "
	}
	return out
}
