package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"

	"streamgrab/host/internal/atomicfile"
	"streamgrab/host/internal/settings"
)

// maxFinished bounds the history. The whole list goes to the browser in one
// message, which Chrome limits to 1 MB.
const maxFinished = 200

// prune drops the oldest finished, failed, and cancelled jobs beyond the limit.
func (m *Manager) prune() {
	excess := -maxFinished
	for _, job := range m.jobs {
		if terminal(job.State) {
			excess++
		}
	}
	for _, job := range slices.Clone(m.jobs) {
		if excess <= 0 {
			return
		}
		if terminal(job.State) && m.active[job.ID] == nil {
			m.remove(job)
			excess--
		}
	}
}

// remove takes a job off the list and discards any partial files it left.
func (m *Manager) remove(job *Job) {
	if job.State != StateDone && m.active[job.ID] == nil {
		m.discard(job)
	}
	m.jobs = slices.DeleteFunc(m.jobs, func(other *Job) bool { return other == job })
	m.emit(Event{Type: "JOB_REMOVED", Job: public(job)})
}

func (m *Manager) persistDir() {
	if m.storePath == "" {
		return
	}
	dir := m.downloadDir
	if err := settings.Update(settings.PathBeside(m.storePath), func(file *settings.File) {
		file.DownloadDir = dir
	}); err != nil {
		log.Printf("save settings: %v", err)
	}
}

func (m *Manager) persist() {
	if m.storePath == "" || m.keepStore {
		return
	}
	saved := make([]Job, len(m.jobs))
	for i, job := range m.jobs {
		saved[i] = *job
	}
	raw, err := json.MarshalIndent(saved, "", "  ")
	if err == nil {
		err = atomicfile.Write(m.storePath, raw, 0o644)
	}
	if err != nil {
		log.Printf("save queue: %v", err)
	}
}

func (m *Manager) load() error {
	if m.storePath == "" {
		return nil
	}
	raw, err := os.ReadFile(m.storePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		m.keepStore = true
		return err
	}
	var jobs []Job
	if err := json.Unmarshal(raw, &jobs); err != nil {
		saved, moveErr := atomicfile.SetAside(m.storePath)
		if moveErr != nil {
			m.keepStore = true
			return errors.Join(err, moveErr)
		}
		return fmt.Errorf("unreadable queue moved to %s: %w", saved, err)
	}
	m.jobs = make([]*Job, len(jobs))
	for i := range jobs {
		job := jobs[i]
		m.jobs[i] = &job
	}
	return nil
}
