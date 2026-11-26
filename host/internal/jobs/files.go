package jobs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var fragmentPattern = regexp.MustCompile(`\.f[0-9]{2,4}\.[a-z0-9]+$`)

func sweepFragments(dir string, older time.Duration, now time.Time) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-older)
	for _, entry := range entries {
		if entry.IsDir() || !isFragment(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func isFragment(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".part") || strings.HasSuffix(lower, ".ytdl") {
		return true
	}
	return fragmentPattern.MatchString(lower)
}
