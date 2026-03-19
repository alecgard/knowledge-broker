package store

import (
	"context"
	"fmt"
	"time"
)

// SaveQueryHistory stores a query and its response in history.
func (s *SQLiteStore) SaveQueryHistory(ctx context.Context, entry QueryHistoryEntry) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO query_history (id, query, mode, response_json, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	`, entry.ID, entry.Query, entry.Mode, entry.ResponseJSON, entry.LatencyMS)
	if err != nil {
		return fmt.Errorf("save query history: %w", err)
	}

	// Cap at 50 entries.
	_, err = s.db.ExecContext(ctx, `
		DELETE FROM query_history WHERE id NOT IN (
			SELECT id FROM query_history ORDER BY created_at DESC LIMIT 50
		)
	`)
	if err != nil {
		return fmt.Errorf("prune query history: %w", err)
	}
	return nil
}

// ListQueryHistory returns the most recent history entries.
func (s *SQLiteStore) ListQueryHistory(ctx context.Context, limit int) ([]QueryHistoryEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, query, mode, response_json, latency_ms, created_at
		 FROM query_history
		 ORDER BY created_at DESC
		 LIMIT ?`,
		limit)
	if err != nil {
		return nil, fmt.Errorf("list query history: %w", err)
	}
	defer rows.Close()

	var entries []QueryHistoryEntry
	for rows.Next() {
		var entry QueryHistoryEntry
		var createdAtStr string
		if err := rows.Scan(&entry.ID, &entry.Query, &entry.Mode, &entry.ResponseJSON, &entry.LatencyMS, &createdAtStr); err != nil {
			return nil, fmt.Errorf("scan history entry: %w", err)
		}
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", createdAtStr); err == nil {
			entry.CreatedAt = t
		} else if t, err := time.Parse(time.RFC3339, createdAtStr); err == nil {
			entry.CreatedAt = t
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

// DeleteQueryHistory deletes a single history entry by ID.
func (s *SQLiteStore) DeleteQueryHistory(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM query_history WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete query history entry: %w", err)
	}
	return nil
}

// ClearQueryHistory deletes all history entries.
func (s *SQLiteStore) ClearQueryHistory(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM query_history`)
	if err != nil {
		return fmt.Errorf("clear query history: %w", err)
	}
	return nil
}
