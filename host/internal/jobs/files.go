package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"streamgrab/host/internal/media"
)

const partialLifetime = 24 * time.Hour

// sweep removes old partial files from every folder the queue has used.
func (m *Manager) sweep() {
	videos := finishedVideos(m.jobs)
	dirs := map[string]bool{m.downloadDir: true}
	for _, job := range m.jobs {
		if job.Dir != "" {
			dirs[job.Dir] = true
		}
	}
	for dir := range dirs {
		sweepFragments(dir, videos, partialLifetime, m.now())
	}
}

// sweepFragments removes stale partial files that belong to one of the given
// videos. The download folder is chosen by the user and can hold files from
// other programs, so anything StreamGrab can't tie to its own job is kept.
func sweepFragments(dir string, videoIDs map[string]bool, older time.Duration, now time.Time) {
	if dir == "" || len(videoIDs) == 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.Add(-older)
	for _, entry := range entries {
		if entry.IsDir() || !media.IsPartial(entry.Name()) || !belongsTo(entry.Name(), videoIDs) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

// discard removes the files an unfinished job wrote. Files that another job
// still needs are kept, as is anything outside the job's folder.
func (m *Manager) discard(job *Job) {
	needed := map[string]bool{}
	for _, other := range m.jobs {
		if other == job {
			continue
		}
		if other.State == StateDone {
			needed[fileKey(other.OutputPath)] = true
		}
		// A failed job's streams are kept for a retry.
		if other.State != StateDone && other.State != StateCancelled {
			for _, file := range other.Files {
				needed[fileKey(file)] = true
			}
		}
	}
	entries, err := os.ReadDir(job.Dir)
	if job.Dir == "" || err != nil {
		return
	}
	for _, file := range job.Files {
		if needed[fileKey(file)] || fileKey(filepath.Dir(file)) != fileKey(job.Dir) {
			continue
		}
		name := filepath.Base(file)
		ext := filepath.Ext(name)
		merging := strings.TrimSuffix(name, ext) + ".temp" + ext
		for _, entry := range entries {
			found := entry.Name()
			if found == name || found == name+".part" || found == name+".ytdl" || found == merging ||
				strings.HasPrefix(found, name+".part-Frag") {
				_ = os.Remove(filepath.Join(job.Dir, found))
			}
		}
	}
	job.Files = nil
}

func fileKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// finishedVideos lists videos with at least one finished job and none that is
// still queued, paused, or downloading.
func finishedVideos(jobs []*Job) map[string]bool {
	out := map[string]bool{}
	for _, job := range jobs {
		if job.VideoID != "" && terminal(job.State) {
			out[job.VideoID] = true
		}
	}
	for _, job := range jobs {
		if !terminal(job.State) {
			delete(out, job.VideoID)
		}
	}
	return out
}

// belongsTo matches the "[videoID]" marker that StreamGrab puts in file names.
func belongsTo(name string, videoIDs map[string]bool) bool {
	for id := range videoIDs {
		if strings.Contains(name, "["+id+"]") {
			return true
		}
	}
	return false
}
