package media

import (
	"regexp"
	"strings"
)

// streamName matches a single downloaded stream, such as "name.f137.mp4" or
// "name.f251-drc.webm".
var streamName = regexp.MustCompile(`\.f[0-9]{1,4}(-[a-z0-9]+)?\.[a-z0-9]+$`)

// IsPartial reports whether a file name is an unfinished download or a stream
// that still has to be merged.
func IsPartial(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".ytdl") || strings.Contains(lower, ".part-frag") {
		return true
	}
	return streamName.MatchString(lower)
}
