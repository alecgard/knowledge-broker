package server

import (
	"sync"
	"time"
)

// JobStatus tracks the state of a background ingestion job.
type JobStatus struct {
	ID         string    `json:"id"`
	SourceType string    `json:"source_type"`
	SourceName string    `json:"source_name"`
	State      string    `json:"state"` // "running", "completed", "failed"
	Progress   int       `json:"progress"`
	Total      int       `json:"total"`
	Error      string    `json:"error,omitempty"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	Added      int       `json:"added"`
	Deleted    int       `json:"deleted"`
	Skipped    int       `json:"skipped"`
}

// JobTracker is an in-memory tracker for background ingestion jobs.
type JobTracker struct {
	mu   sync.RWMutex
	jobs map[string]*JobStatus
}

// NewJobTracker creates a new job tracker.
func NewJobTracker() *JobTracker {
	return &JobTracker{jobs: make(map[string]*JobStatus)}
}

// Start registers a new running job. Returns false if a job with the same ID
// is already running.
func (t *JobTracker) Start(sourceType, sourceName string) (string, bool) {
	id := sourceType + "/" + sourceName
	t.mu.Lock()
	defer t.mu.Unlock()
	if j, ok := t.jobs[id]; ok && j.State == "running" {
		return id, false
	}
	t.jobs[id] = &JobStatus{
		ID:         id,
		SourceType: sourceType,
		SourceName: sourceName,
		State:      "running",
		StartedAt:  time.Now(),
	}
	return id, true
}

// Update sets the progress of a running job.
func (t *JobTracker) Update(id string, progress, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if j, ok := t.jobs[id]; ok && j.State == "running" {
		j.Progress = progress
		j.Total = total
	}
}

// Finish marks a job as completed or failed.
func (t *JobTracker) Finish(id string, added, deleted, skipped int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	j, ok := t.jobs[id]
	if !ok {
		return
	}
	j.FinishedAt = time.Now()
	j.Added = added
	j.Deleted = deleted
	j.Skipped = skipped
	if err != nil {
		j.State = "failed"
		j.Error = err.Error()
	} else {
		j.State = "completed"
	}
}

// Get returns the status of a specific job.
func (t *JobTracker) Get(id string) (JobStatus, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	j, ok := t.jobs[id]
	if !ok {
		return JobStatus{}, false
	}
	return *j, true
}

// List returns all job statuses.
func (t *JobTracker) List() []JobStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]JobStatus, 0, len(t.jobs))
	for _, j := range t.jobs {
		result = append(result, *j)
	}
	return result
}

// IsRunning returns true if the given source has a running job.
func (t *JobTracker) IsRunning(sourceType, sourceName string) bool {
	id := sourceType + "/" + sourceName
	t.mu.RLock()
	defer t.mu.RUnlock()
	j, ok := t.jobs[id]
	return ok && j.State == "running"
}
