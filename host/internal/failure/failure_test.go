package failure

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClassify(t *testing.T) {
	cases := map[string]string{
		"ERROR: unable to download video data: <urlopen error [Errno 11001] getaddrinfo failed>": "NETWORK_ERROR",
		"ERROR: [Errno 10054] Connection reset by peer":                                          "NETWORK_ERROR",
		"ERROR: unable to download video data: HTTP Error 503: Service Unavailable":              "NETWORK_ERROR",
		"ERROR: [youtube] x: Read timed out.":                                                    "NETWORK_ERROR",
		"ERROR: unable to write data: [Errno 28] No space left on device":                        "DISK_FULL",
		"ERROR: Private video. Title: Deep Space":                                                "EXTRACTION_FAILED",
		"[download] Destination: Network Error drm.webm\nERROR: Video unavailable":               "EXTRACTION_FAILED",
		"ERROR: Postprocessing: Conversion failed!":                                              "MUX_FAILED",
		"ERROR: Sign in to confirm your age":                                                     "AGE_RESTRICTED",
		"ERROR: [youtube] x: Sign in to confirm you’re not a bot. Use --cookies":                 "BOT_CHECK",
		"ERROR: [youtube] x: Sign in to confirm you're not a bot":                                "BOT_CHECK",
	}
	for text, want := range cases {
		if got := Classify(text).Code; got != want {
			t.Errorf("%q = %s, want %s", text, got, want)
		}
	}
}

func TestTruncateKeepsWholeCharacters(t *testing.T) {
	got := Truncate("a"+strings.Repeat("日", 40), 80)
	if !utf8.ValidString(got) || len(got) > 80 || len(got) < 77 {
		t.Fatalf("%d %q", len(got), got)
	}
}
