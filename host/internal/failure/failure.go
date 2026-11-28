// Package failure turns the output of yt-dlp and FFmpeg into the errors shown
// to the user.
package failure

import (
	"strings"
	"unicode/utf8"

	"streamgrab/host/internal/protocol"
)

var rules = []struct {
	code    string
	message string
	signs   []string
}{
	{"DRM_PROTECTED", "StreamGrab doesn't support DRM content.", []string{"drm"}},
	// Must come before the age rule, which also matches "sign in to confirm".
	// The sign leaves out "you're" because yt-dlp may print a curly apostrophe.
	{"BOT_CHECK", "YouTube asked to confirm you're not a bot. Try again later or from another network.",
		[]string{"not a bot"}},
	{"AGE_RESTRICTED", "This video is age-restricted. Browser cookies are not enabled in this version.",
		[]string{"confirm your age", "age-restricted", "sign in to confirm"}},
	{"LIVE_STREAM", "This live stream can't be downloaded in this version.", []string{"live event", "is live"}},
	{"EXTRACTION_FAILED", "StreamGrab couldn't read this video because Deno was not available to yt-dlp.",
		[]string{"javascript runtime", "challenge solving failed", "signature solving failed"}},
	{"DISK_FULL", "The download disk is full.",
		[]string{"no space left on device", "errno 28", "not enough space on the disk", "disk full", "disk is full", "disk quota exceeded"}},
	{"NETWORK_ERROR", "The download stopped because the network failed.",
		[]string{
			"timed out", "getaddrinfo failed", "name or service not known", "temporary failure in name resolution",
			"connection reset", "connection refused", "connection aborted", "remote end closed connection",
			"network is unreachable", "network down", "network error", "network request failed",
			"errno 10054", "errno 10060", "errno 10061", "errno 11001", "winerror 10054", "winerror 10060",
			"incompleteread", "unexpected_eof", "unable to connect",
			"http error 500", "http error 502", "http error 503", "http error 504",
		}},
	{"MUX_FAILED", "Muxing failed. The partial streams were kept.", []string{"postprocessing:", "muxing", "mux failed"}},
}

// Classify reads tool output. When the output has "ERROR:" lines only those are
// considered, so that file names and titles elsewhere can't change the result.
func Classify(text string) *protocol.Error {
	relevant := errorLines(text)
	lower := strings.ToLower(relevant)
	for _, rule := range rules {
		for _, sign := range rule.signs {
			if strings.Contains(lower, sign) {
				return &protocol.Error{Code: rule.code, Message: rule.message}
			}
		}
	}
	message := "The download failed. Update yt-dlp and try again."
	if reason := reason(relevant); reason != "" {
		message += " " + reason
	}
	return &protocol.Error{Code: "EXTRACTION_FAILED", Message: message}
}

// Truncate shortens text to at most limit bytes without splitting a character.
func Truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

func errorLines(text string) string {
	var found []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "ERROR:") {
			found = append(found, strings.TrimSpace(line))
		}
	}
	if len(found) == 0 {
		return text
	}
	return strings.Join(found, "\n")
}

// reason is the first yt-dlp error line that is safe to show.
func reason(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "ERROR:") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "ERROR:"))
		if line == "" || strings.Contains(line, "http://") || strings.Contains(line, "https://") {
			continue
		}
		return Truncate(line, 160)
	}
	return ""
}
