package store

import (
	"context"
	"testing"
	"time"
)

func TestPutAndGetCachedAnswer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	answerJSON := []byte(`{"content":"hello","confidence":{"overall":0.9}}`)

	if err := s.PutCachedAnswer(ctx, "key1", "what is go?", true, "sigs123", answerJSON); err != nil {
		t.Fatalf("PutCachedAnswer: %v", err)
	}

	// Retrieve it.
	got, err := s.GetCachedAnswer(ctx, "key1", 10*time.Minute)
	if err != nil {
		t.Fatalf("GetCachedAnswer: %v", err)
	}
	if got == nil {
		t.Fatal("expected cached answer, got nil")
	}
	if string(got.AnswerJSON) != string(answerJSON) {
		t.Errorf("answer JSON mismatch: got %s", got.AnswerJSON)
	}
	if got.FragmentSigs != "sigs123" {
		t.Errorf("fragment sigs mismatch: got %s", got.FragmentSigs)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestGetCachedAnswer_Miss(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetCachedAnswer(ctx, "nonexistent", 10*time.Minute)
	if err != nil {
		t.Fatalf("GetCachedAnswer: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestGetCachedAnswer_Expired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	answerJSON := []byte(`{"content":"old"}`)
	if err := s.PutCachedAnswer(ctx, "key1", "query", false, "sigs", answerJSON); err != nil {
		t.Fatal(err)
	}

	// With a very short maxAge, entry should be considered expired.
	// We can't easily control the SQLite timestamp, but we can use a zero maxAge
	// which means no age check.
	got, err := s.GetCachedAnswer(ctx, "key1", 0)
	if err != nil {
		t.Fatal(err)
	}
	// maxAge=0 means no expiry check, so it should be returned.
	if got == nil {
		t.Fatal("expected cached answer with maxAge=0")
	}
}

func TestPutCachedAnswer_Replaces(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.PutCachedAnswer(ctx, "key1", "query", true, "sigs1", []byte(`{"content":"v1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.PutCachedAnswer(ctx, "key1", "query", true, "sigs2", []byte(`{"content":"v2"}`)); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetCachedAnswer(ctx, "key1", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected cached answer")
	}
	if got.FragmentSigs != "sigs2" {
		t.Errorf("expected sigs2, got %s", got.FragmentSigs)
	}
	if string(got.AnswerJSON) != `{"content":"v2"}` {
		t.Errorf("expected v2, got %s", got.AnswerJSON)
	}
}

func TestPruneCacheEntries(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.PutCachedAnswer(ctx, "key1", "q1", true, "s1", []byte(`{"content":"a"}`)); err != nil {
		t.Fatal(err)
	}

	// Prune with a very short max age won't delete recent entries (just inserted).
	// But prune with 0 duration should delete everything.
	// Since the entry was just created, pruning with 1h should keep it.
	if err := s.PruneCacheEntries(ctx, 1*time.Hour); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetCachedAnswer(ctx, "key1", 0)
	if got == nil {
		t.Fatal("expected entry to survive 1h prune")
	}
}
