package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/001_initial.sql
var migrationSQL string

// SQLiteStore implements Store using SQLite and sqlite-vec.
type SQLiteStore struct {
	db           *sql.DB
	embeddingDim int
}

func init() {
	vec.Auto()
}

// NewSQLiteStore opens (or creates) a SQLite database at dbPath, runs
// migrations, creates the sqlite-vec virtual table, and validates metadata.
func NewSQLiteStore(dbPath string, embeddingDim int) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Run schema migrations.
	if _, err := db.Exec(migrationSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// Create the sqlite-vec virtual table.
	vtableSQL := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS fragment_embeddings USING vec0(
			fragment_id TEXT PRIMARY KEY,
			embedding float[%d]
		);`, embeddingDim)
	if _, err := db.Exec(vtableSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("create vec0 virtual table: %w", err)
	}

	// Validate or store embedding dimension in metadata.
	if err := validateOrSetMeta(db, "embedding_dim", fmt.Sprintf("%d", embeddingDim)); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, embeddingDim: embeddingDim}, nil
}

func validateOrSetMeta(db *sql.DB, key, expected string) error {
	var existing string
	err := db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&existing)
	if err == sql.ErrNoRows {
		_, err = db.Exec("INSERT INTO metadata (key, value) VALUES (?, ?)", key, expected)
		return err
	}
	if err != nil {
		return fmt.Errorf("read metadata %s: %w", key, err)
	}
	if existing != expected {
		return fmt.Errorf("metadata %s mismatch: db has %q, expected %q", key, existing, expected)
	}
	return nil
}

// serializeEmbedding converts a float32 slice to a little-endian byte slice.
func serializeEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// deserializeEmbedding converts a little-endian byte slice to a float32 slice.
func deserializeEmbedding(b []byte) []float32 {
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// scanFragment scans a fragment row from the standard column set:
// id, content, source_type, source_name, source_path, source_uri,
// last_modified, author, file_type, checksum, confidence_adj.
// Additional columns (e.g., distance, embedding) must be handled by the caller
// via the extra parameter.
func scanFragment(scanner interface{ Scan(...any) error }, extra ...any) (model.SourceFragment, error) {
	var f model.SourceFragment
	var lastMod string
	dest := []any{
		&f.ID, &f.Content, &f.SourceType, &f.SourceName, &f.SourcePath, &f.SourceURI,
		&lastMod, &f.Author, &f.FileType, &f.Checksum, &f.ConfidenceAdj,
	}
	dest = append(dest, extra...)
	if err := scanner.Scan(dest...); err != nil {
		return model.SourceFragment{}, err
	}
	f.LastModified, _ = time.Parse(time.RFC3339, lastMod)
	return f, nil
}

// UpsertFragments inserts or replaces fragments and their embeddings.
func (s *SQLiteStore) UpsertFragments(ctx context.Context, fragments []model.SourceFragment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	fragStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO fragments
			(id, content, source_type, source_name, source_path, source_uri, last_modified, author, file_type, checksum, confidence_adj, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`)
	if err != nil {
		return fmt.Errorf("prepare fragment stmt: %w", err)
	}
	defer fragStmt.Close()

	embDelStmt, err := tx.PrepareContext(ctx, `
		DELETE FROM fragment_embeddings WHERE fragment_id = ?
	`)
	if err != nil {
		return fmt.Errorf("prepare embedding del stmt: %w", err)
	}
	defer embDelStmt.Close()

	embInsStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO fragment_embeddings (fragment_id, embedding)
		VALUES (?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare embedding ins stmt: %w", err)
	}
	defer embInsStmt.Close()

	for _, f := range fragments {
		_, err := fragStmt.ExecContext(ctx,
			f.ID, f.Content, f.SourceType, f.SourceName, f.SourcePath, f.SourceURI,
			f.LastModified.UTC().Format(time.RFC3339),
			f.Author, f.FileType, f.Checksum, f.ConfidenceAdj,
		)
		if err != nil {
			return fmt.Errorf("upsert fragment %s: %w", f.ID, err)
		}

		if len(f.Embedding) > 0 {
			// vec0 virtual tables don't support INSERT OR REPLACE,
			// so delete any existing row first.
			if _, err = embDelStmt.ExecContext(ctx, f.ID); err != nil {
				return fmt.Errorf("delete old embedding %s: %w", f.ID, err)
			}
			if _, err = embInsStmt.ExecContext(ctx, f.ID, serializeEmbedding(f.Embedding)); err != nil {
				return fmt.Errorf("upsert embedding %s: %w", f.ID, err)
			}
		}
	}

	return tx.Commit()
}

// SearchByVector finds the nearest fragments to the given embedding vector.
func (s *SQLiteStore) SearchByVector(ctx context.Context, embedding []float32, limit int) ([]model.SourceFragment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.last_modified, f.author, f.file_type, f.checksum, f.confidence_adj,
		       fe.distance
		FROM fragment_embeddings fe
		INNER JOIN fragments f ON f.id = fe.fragment_id
		WHERE fe.embedding MATCH ? AND k = ?
		ORDER BY fe.distance
	`, serializeEmbedding(embedding), limit)
	if err != nil {
		return nil, fmt.Errorf("search by vector: %w", err)
	}
	defer rows.Close()

	var results []model.SourceFragment
	for rows.Next() {
		var distance float64
		f, err := scanFragment(rows, &distance)
		if err != nil {
			return nil, fmt.Errorf("scan fragment: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// SearchByVectorFiltered finds the nearest fragments filtered by source names.
// If sourceNames is empty, it delegates to SearchByVector.
// Since sqlite-vec doesn't support WHERE clauses on joined columns in the
// virtual table query, we over-fetch from the vector index and post-filter.
func (s *SQLiteStore) SearchByVectorFiltered(ctx context.Context, embedding []float32, limit int, sourceNames []string) ([]model.SourceFragment, error) {
	if len(sourceNames) == 0 {
		return s.SearchByVector(ctx, embedding, limit)
	}

	// Over-fetch to account for filtering. We fetch limit*5 candidates from
	// the vector index and then filter by source_name.
	overFetch := limit * 5
	if overFetch < 50 {
		overFetch = 50
	}

	placeholders := make([]string, len(sourceNames))
	args := []interface{}{serializeEmbedding(embedding), overFetch}
	for i, name := range sourceNames {
		placeholders[i] = "?"
		args = append(args, name)
	}

	query := fmt.Sprintf(`
		SELECT f.id, f.content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.last_modified, f.author, f.file_type, f.checksum, f.confidence_adj,
		       fe.distance
		FROM fragment_embeddings fe
		INNER JOIN fragments f ON f.id = fe.fragment_id
		WHERE fe.embedding MATCH ? AND k = ?
		  AND f.source_name IN (%s)
		ORDER BY fe.distance
		LIMIT ?
	`, strings.Join(placeholders, ","))
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search by vector filtered: %w", err)
	}
	defer rows.Close()

	var results []model.SourceFragment
	for rows.Next() {
		var distance float64
		f, err := scanFragment(rows, &distance)
		if err != nil {
			return nil, fmt.Errorf("scan fragment: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// GetFragments retrieves fragments by their IDs.
func (s *SQLiteStore) GetFragments(ctx context.Context, ids []string) ([]model.SourceFragment, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(`
		SELECT id, content, source_type, source_name, source_path, source_uri,
		       last_modified, author, file_type, checksum, confidence_adj
		FROM fragments
		WHERE id IN (%s)
	`, strings.Join(placeholders, ","))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get fragments: %w", err)
	}
	defer rows.Close()

	var results []model.SourceFragment
	for rows.Next() {
		f, err := scanFragment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan fragment: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// GetChecksums returns a map of source_path -> checksum for all fragments of the given source type and name.
func (s *SQLiteStore) GetChecksums(ctx context.Context, sourceType, sourceName string) (map[string]string, error) {
	q := "SELECT source_path, checksum FROM fragments WHERE source_type = ?"
	args := []interface{}{sourceType}
	if sourceName != "" {
		q += " AND source_name = ?"
		args = append(args, sourceName)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("get checksums: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var path, checksum string
		if err := rows.Scan(&path, &checksum); err != nil {
			return nil, fmt.Errorf("scan checksum: %w", err)
		}
		result[path] = checksum
	}
	return result, rows.Err()
}

// DeleteByPaths removes fragments matching the given source type, name, and paths.
func (s *SQLiteStore) DeleteByPaths(ctx context.Context, sourceType, sourceName string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	placeholders := make([]string, len(paths))
	args := make([]interface{}, 0, len(paths)+2)
	args = append(args, sourceType)
	if sourceName != "" {
		args = append(args, sourceName)
	}
	for i, p := range paths {
		placeholders[i] = "?"
		args = append(args, p)
	}
	inClause := strings.Join(placeholders, ",")

	nameFilter := ""
	if sourceName != "" {
		nameFilter = " AND source_name = ?"
	}

	// Delete from fragment_embeddings first (referencing fragment IDs).
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM fragment_embeddings
		WHERE fragment_id IN (
			SELECT id FROM fragments WHERE source_type = ?%s AND source_path IN (%s)
		)
	`, nameFilter, inClause), args...)
	if err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}

	// Delete from fragments.
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM fragments WHERE source_type = ?%s AND source_path IN (%s)
	`, nameFilter, inClause), args...)
	if err != nil {
		return fmt.Errorf("delete fragments: %w", err)
	}

	return tx.Commit()
}

// ExportFragments returns all fragments joined with their embeddings.
func (s *SQLiteStore) ExportFragments(ctx context.Context) ([]model.SourceFragment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.last_modified, f.author, f.file_type, f.checksum, f.confidence_adj,
		       fe.embedding
		FROM fragments f
		INNER JOIN fragment_embeddings fe ON fe.fragment_id = f.id
	`)
	if err != nil {
		return nil, fmt.Errorf("export fragments: %w", err)
	}
	defer rows.Close()

	var results []model.SourceFragment
	for rows.Next() {
		var embBytes []byte
		f, err := scanFragment(rows, &embBytes)
		if err != nil {
			return nil, fmt.Errorf("scan fragment: %w", err)
		}
		f.Embedding = deserializeEmbedding(embBytes)
		results = append(results, f)
	}
	return results, rows.Err()
}

// RegisterSource inserts or updates a registered source.
func (s *SQLiteStore) RegisterSource(ctx context.Context, src model.Source) error {
	configJSON, err := json.Marshal(src.Config)
	if err != nil {
		return fmt.Errorf("marshal source config: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO sources (source_type, source_name, config, last_ingest)
		VALUES (?, ?, ?, ?)
	`, src.SourceType, src.SourceName, string(configJSON), src.LastIngest.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("register source: %w", err)
	}
	return nil
}

// ListSources returns all registered sources.
func (s *SQLiteStore) ListSources(ctx context.Context) ([]model.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT source_type, source_name, config, last_ingest FROM sources ORDER BY source_type, source_name")
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	var sources []model.Source
	for rows.Next() {
		var src model.Source
		var configJSON string
		var lastIngest sql.NullString
		if err := rows.Scan(&src.SourceType, &src.SourceName, &configJSON, &lastIngest); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		if err := json.Unmarshal([]byte(configJSON), &src.Config); err != nil {
			return nil, fmt.Errorf("unmarshal source config: %w", err)
		}
		if lastIngest.Valid {
			src.LastIngest, _ = time.Parse(time.RFC3339, lastIngest.String)
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// CountFragmentsBySource returns a map of "source_type/source_name" to fragment count.
func (s *SQLiteStore) CountFragmentsBySource(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_type || '/' || source_name AS key, COUNT(*) FROM fragments GROUP BY source_type, source_name`)
	if err != nil {
		return nil, fmt.Errorf("count fragments by source: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, fmt.Errorf("scan count: %w", err)
		}
		result[key] = count
	}
	return result, rows.Err()
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
