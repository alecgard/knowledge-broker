package server

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestJobTrackerStartAndFinish(t *testing.T) {
	tracker := NewJobTracker()

	id, _, ok := tracker.Start(context.Background(), "git", "owner/repo")
	if !ok {
		t.Fatal("expected Start to succeed")
	}
	if id != "git/owner/repo" {
		t.Fatalf("expected id git/owner/repo, got %s", id)
	}

	if !tracker.IsRunning("git", "owner/repo") {
		t.Fatal("expected job to be running")
	}

	tracker.Update(id, 5, 10)
	job, ok := tracker.Get(id)
	if !ok {
		t.Fatal("expected Get to find job")
	}
	if job.Progress != 5 || job.Total != 10 {
		t.Fatalf("expected progress 5/10, got %d/%d", job.Progress, job.Total)
	}

	tracker.Finish(id, 8, 1, 1, nil)
	job, _ = tracker.Get(id)
	if job.State != "completed" {
		t.Fatalf("expected completed, got %s", job.State)
	}
	if job.Added != 8 || job.Deleted != 1 || job.Skipped != 1 {
		t.Fatalf("unexpected result: added=%d deleted=%d skipped=%d", job.Added, job.Deleted, job.Skipped)
	}
	if job.FinishedAt == nil {
		t.Fatal("expected FinishedAt to be set")
	}
}

func TestJobTrackerFinishWithError(t *testing.T) {
	tracker := NewJobTracker()
	id, _, _ := tracker.Start(context.Background(), "slack", "workspace")
	tracker.Finish(id, 0, 0, 0, fmt.Errorf("connection refused"))

	job, _ := tracker.Get(id)
	if job.State != "failed" {
		t.Fatalf("expected failed, got %s", job.State)
	}
	if job.Error != "connection refused" {
		t.Fatalf("expected error message, got %q", job.Error)
	}
}

func TestJobTrackerRejectDuplicateRunning(t *testing.T) {
	tracker := NewJobTracker()
	tracker.Start(context.Background(), "git", "owner/repo")

	_, _, ok := tracker.Start(context.Background(), "git", "owner/repo")
	if ok {
		t.Fatal("expected second Start to be rejected while job is running")
	}
}

func TestJobTrackerAllowRestartAfterCompletion(t *testing.T) {
	tracker := NewJobTracker()
	id, _, _ := tracker.Start(context.Background(), "git", "owner/repo")
	tracker.Finish(id, 5, 0, 0, nil)

	_, _, ok := tracker.Start(context.Background(), "git", "owner/repo")
	if !ok {
		t.Fatal("expected Start to succeed after previous job completed")
	}
}

func TestJobTrackerList(t *testing.T) {
	tracker := NewJobTracker()
	tracker.Start(context.Background(), "git", "repo1")
	tracker.Start(context.Background(), "slack", "workspace")

	jobs := tracker.List()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
}

func TestJobTrackerGetNotFound(t *testing.T) {
	tracker := NewJobTracker()
	_, ok := tracker.Get("nonexistent")
	if ok {
		t.Fatal("expected Get to return false for nonexistent job")
	}
}

func TestJobTrackerIsRunningFalse(t *testing.T) {
	tracker := NewJobTracker()
	if tracker.IsRunning("git", "nonexistent") {
		t.Fatal("expected IsRunning to return false for nonexistent job")
	}
}

func TestJobTrackerConcurrentAccess(t *testing.T) {
	tracker := NewJobTracker()
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("repo-%d", n)
			id, _, ok := tracker.Start(context.Background(), "git", name)
			if ok {
				tracker.Update(id, 1, 10)
				tracker.Finish(id, 5, 0, 5, nil)
			}
			tracker.List()
			tracker.IsRunning("git", name)
		}(i)
	}

	wg.Wait()

	jobs := tracker.List()
	if len(jobs) != 20 {
		t.Fatalf("expected 20 jobs, got %d", len(jobs))
	}
}

func TestJobTrackerCancel(t *testing.T) {
	tracker := NewJobTracker()
	_, ctx, _ := tracker.Start(context.Background(), "git", "owner/repo")

	// Context should not be cancelled yet.
	if ctx.Err() != nil {
		t.Fatal("expected context to not be cancelled")
	}

	// Cancel the job.
	if !tracker.Cancel("git", "owner/repo") {
		t.Fatal("expected Cancel to return true")
	}

	// Context should now be cancelled.
	if ctx.Err() == nil {
		t.Fatal("expected context to be cancelled")
	}

	// Cancelling a non-running job should return false.
	tracker.Finish("git/owner/repo", 0, 0, 0, ctx.Err())
	if tracker.Cancel("git", "owner/repo") {
		t.Fatal("expected Cancel to return false for completed job")
	}
}
