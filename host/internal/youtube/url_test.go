package youtube

import "testing"

func TestParseAllowed(t *testing.T) {
	cases := []struct {
		raw  string
		id   string
		kind string
	}{
		{"https://www.youtube.com/watch?v=abcdefghijk", "abcdefghijk", "watch"},
		{"https://m.youtube.com/watch?v=abcdefghijk&list=PL", "abcdefghijk", "watch"},
		{"https://music.youtube.com/watch?v=abcdefghijk", "abcdefghijk", "watch"},
		{"https://www.youtube.com/shorts/abcdefghijk", "abcdefghijk", "shorts"},
		{"https://www.youtube.com/live/abcdefghijk", "abcdefghijk", "live"},
		{"https://youtu.be/abcdefghijk", "abcdefghijk", "watch"},
	}
	for _, tc := range cases {
		got, err := Parse(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if got.VideoID != tc.id || got.Kind != tc.kind {
			t.Fatalf("%s: %+v", tc.raw, got)
		}
		if got.URL != "https://www.youtube.com/watch?v="+tc.id {
			t.Fatalf("canonical url = %s", got.URL)
		}
	}
}

func TestParseRejected(t *testing.T) {
	rejected := []string{
		"https://example.com/watch?v=abcdefghijk",
		"https://www.youtube.com/feed/subscriptions",
		"https://www.youtube.com/playlist?list=PL123",
		"https://www.youtube.com/watch?v=short",
		"https://www.youtube.com/watch?v=bad$(id)xxx",
		"file:///etc/passwd",
		"",
	}
	for _, raw := range rejected {
		if _, err := Parse(raw); err == nil {
			t.Fatalf("expected rejection for %q", raw)
		}
	}
}

func TestFormatID(t *testing.T) {
	if !ValidFormatID("137") || !ValidFormatID("140") || !ValidFormatID("") {
		t.Fatal("expected plain format ids to pass")
	}
	if ValidFormatID("137;rm") || ValidFormatID("a b") {
		t.Fatal("expected unsafe format ids to fail")
	}
}
