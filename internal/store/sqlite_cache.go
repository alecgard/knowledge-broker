package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// GetCachedAnswer retrieves a cached answer by cache key.
// Returns nil if not found or older than maxAge.
func (s *SQLiteStore) GetCachedAnswer(ctx context.Context, cacheKey string, maxAge time.Duration) (*CachedAnswer, error) {
	var answerJSON, fragmentSigs, createdAtStr string
	err := s.db.QueryRowContext(ctx,
		`SELECT answer_json, fragment_sigs, created_at FROM query_cache WHERE cache_key = ?`,
		cacheKey,
	).Scan(&answerJSON, &fragmentSigs, &createdAtStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cached answer: %w", err)
	}

	createdAt, _ := time.Parse("2006-01-02T15:04:05.000Z", createdAtStr)
	if createdAt.IsZero() {
		// Try alternate format without millis.
		createdAt, _ = time.Parse(time.RFC3339, createdAtStr)
	}

	if maxAge > 0 && time.Since(createdAt) > maxAge {
		return nil, nil
	}

	return &CachedAnswer{
		AnswerJSON:   []byte(answerJSON),
		FragmentSigs: fragmentSigs,
		CreatedAt:    createdAt,
	}, nil
}

// PutCachedAnswer stores a query answer in the disk cache.
func (s *SQLiteStore) PutCachedAnswer(ctx context.Context, cacheKey, queryText string, concise bool, fragmentSigs string, answer []byte) error {
	conciseInt := 0
	if concise {
		conciseInt = 1
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO query_cache (cache_key, query_text, concise, fragment_sigs, answer_json, created_at)
		VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	`, cacheKey, queryText, conciseInt, fragmentSigs, string(answer))
	if err != nil {
		return fmt.Errorf("put cached answer: %w", err)
	}
	return nil
}

// PruneCacheEntries deletes entries older than maxAge.
func (s *SQLiteStore) PruneCacheEntries(ctx context.Context, maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge).UTC().Format("2006-01-02T15:04:05.000Z")
	_, err := s.db.ExecContext(ctx, `DELETE FROM query_cache WHERE created_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("prune cache entries: %w", err)
	}
	return nil
}
