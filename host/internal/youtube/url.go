package youtube

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Parsed struct {
	VideoID string
	URL     string
	Kind    string
}

func Parse(raw string) (Parsed, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Parsed{}, fmt.Errorf("invalid url")
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return Parsed{}, fmt.Errorf("unsupported url scheme")
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "youtu.be":
		id := strings.Trim(parsed.Path, "/")
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
		if !idPattern.MatchString(id) {
			return Parsed{}, fmt.Errorf("unsupported url")
		}
		return canonical(id, "watch"), nil
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		return parseYouTubeHost(parsed)
	default:
		return Parsed{}, fmt.Errorf("unsupported url")
	}
}

func parseYouTubeHost(parsed *url.URL) (Parsed, error) {
	path := strings.Trim(parsed.EscapedPath(), "/")
	switch {
	case path == "watch":
		id := parsed.Query().Get("v")
		if !idPattern.MatchString(id) {
			return Parsed{}, fmt.Errorf("unsupported url")
		}
		return canonical(id, "watch"), nil
	case strings.HasPrefix(path, "shorts/"):
		id := strings.TrimPrefix(path, "shorts/")
		id = strings.Split(id, "/")[0]
		if !idPattern.MatchString(id) {
			return Parsed{}, fmt.Errorf("unsupported url")
		}
		return canonical(id, "shorts"), nil
	case strings.HasPrefix(path, "live/"):
		id := strings.TrimPrefix(path, "live/")
		id = strings.Split(id, "/")[0]
		if !idPattern.MatchString(id) {
			return Parsed{}, fmt.Errorf("unsupported url")
		}
		return canonical(id, "live"), nil
	default:
		return Parsed{}, fmt.Errorf("unsupported url")
	}
}

func canonical(id, kind string) Parsed {
	return Parsed{
		VideoID: id,
		Kind:    kind,
		URL:     "https://www.youtube.com/watch?v=" + id,
	}
}

func ValidFormatID(id string) bool {
	if id == "" {
		return true
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
