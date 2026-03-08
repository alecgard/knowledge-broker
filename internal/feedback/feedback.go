package feedback

import (
	"context"
	"fmt"

	"github.com/knowledge-broker/knowledge-broker/internal/model"
	"github.com/knowledge-broker/knowledge-broker/internal/store"
)

// Service handles feedback operations.
type Service struct {
	store store.Store
}

// NewService creates a feedback service.
func NewService(s store.Store) *Service {
	return &Service{store: s}
}

// Submit records feedback for a fragment.
func (s *Service) Submit(ctx context.Context, fb model.Feedback) error {
	if fb.FragmentID == "" {
		return fmt.Errorf("fragment_id is required")
	}

	switch fb.Type {
	case model.FeedbackCorrection, model.FeedbackChallenge, model.FeedbackConfirmation:
		// valid
	default:
		return fmt.Errorf("invalid feedback type: %q", fb.Type)
	}

	if fb.Type == model.FeedbackCorrection && fb.Content == "" {
		return fmt.Errorf("content is required for corrections")
	}

	return s.store.RecordFeedback(ctx, fb)
}
