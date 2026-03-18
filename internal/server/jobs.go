package server

import (
	"context"
	"sync"
	"time"
)

// JobStatus tracks the state of a background ingestion job.
type JobStatus struct {
	ID         string     `json:"id"`
	SourceType string     `json:"source_type"`
	SourceName string     `json:"source_name"`
	State      string     `json:"state"` // "running", "completed", "failed"
	Progress   int        `json:"progress"`
	Total      int        `json:"total"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Added      int        `json:"added"`
	Deleted    int        `json:"deleted"`
	Skipped    int        `json:"skipped"`
}

// jobEntry is the internal representation that pairs status with a cancel func.
type jobEntry struct {
	status *JobStatus
	cancel context.CancelFunc
}

// JobTracker is an in-memory tracker for background ingestion jobs.
type JobTracker struct {
	mu   sync.RWMutex
	jobs map[string]*jobEntry
}

// NewJobTracker creates a new job tracker.
func NewJobTracker() *JobTracker {
	return &JobTracker{jobs: make(map[string]*jobEntry)}
}

// Start registers a new running job and returns a context that will be
// cancelled if the job is cancelled via Cancel. Returns false if a job
// with the same ID is already running.
func (t *JobTracker) Start(parentCtx context.Context, sourceType, sourceName string) (string, context.Context, bool) {
	id := sourceType + "/" + sourceName
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.jobs[id]; ok && e.status.State == "running" {
		return id, nil, false
	}
	ctx, cancel := context.WithCancel(parentCtx)
	t.jobs[id] = &jobEntry{
		status: &JobStatus{
			ID:         id,
			SourceType: sourceType,
			SourceName: sourceName,
			State:      "running",
			StartedAt:  time.Now(),
		},
		cancel: cancel,
	}
	return id, ctx, true
}

// Update sets the progress of a running job.
func (t *JobTracker) Update(id string, progress, total int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e, ok := t.jobs[id]; ok && e.status.State == "running" {
		e.status.Progress = progress
		e.status.Total = total
	}
}

// Finish marks a job as completed or failed.
func (t *JobTracker) Finish(id string, added, deleted, skipped int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.jobs[id]
	if !ok {
		return
	}
	now := time.Now()
	e.status.FinishedAt = &now
	e.status.Added = added
	e.status.Deleted = deleted
	e.status.Skipped = skipped
	if err != nil {
		e.status.State = "failed"
		e.status.Error = err.Error()
	} else {
		e.status.State = "completed"
	}
}

// Cancel cancels a running job's context. Returns true if a running job was found.
func (t *JobTracker) Cancel(sourceType, sourceName string) bool {
	id := sourceType + "/" + sourceName
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.jobs[id]
	if !ok || e.status.State != "running" {
		return false
	}
	e.cancel()
	return true
}

// Get returns the status of a specific job.
func (t *JobTracker) Get(id string) (JobStatus, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.jobs[id]
	if !ok {
		return JobStatus{}, false
	}
	return *e.status, true
}

// List returns all job statuses.
func (t *JobTracker) List() []JobStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make([]JobStatus, 0, len(t.jobs))
	for _, e := range t.jobs {
		result = append(result, *e.status)
	}
	return result
}

// IsRunning returns true if the given source has a running job.
func (t *JobTracker) IsRunning(sourceType, sourceName string) bool {
	id := sourceType + "/" + sourceName
	t.mu.RLock()
	defer t.mu.RUnlock()
	e, ok := t.jobs[id]
	return ok && e.status.State == "running"
}
