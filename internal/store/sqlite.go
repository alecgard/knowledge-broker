package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/knowledge-broker/knowledge-broker/pkg/model"
	sqlite3 "github.com/mattn/go-sqlite3"
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
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// Run all idempotent initialization (migrations, virtual tables,
	// metadata) with retry so that concurrent openers don't fail with
	// "database is locked" during startup.
	if err := withRetry(5, func() error { return initSchema(db, embeddingDim) }); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, embeddingDim: embeddingDim}, nil
}

// schemaReady checks whether the database schema is already initialized and
// the stored embedding dimension matches. When true, we can skip all write
// operations in initSchema, avoiding write-lock contention with concurrent
// writers (e.g. an in-progress ingestion).
func schemaReady(db *sql.DB, embeddingDim int) bool {
	var val string
	err := db.QueryRow("SELECT value FROM metadata WHERE key = 'embedding_dim'").Scan(&val)
	return err == nil && val == fmt.Sprintf("%d", embeddingDim)
}

// initSchema runs migrations, creates virtual tables, and validates metadata.
// All operations are idempotent so safe to retry. If the schema is already
// fully initialized, only a read is performed (no write lock needed).
func initSchema(db *sql.DB, embeddingDim int) error {
	// Fast path: if schema already exists with the correct embedding dim,
	// skip all writes. This lets read-only callers (e.g. "kb sources list")
	// open the store without acquiring a write lock.
	if schemaReady(db, embeddingDim) {
		return nil
	}

	// Run schema migrations.
	if _, err := db.Exec(migrationSQL); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}



	// Create the sqlite-vec virtual table.
	vtableSQL := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS fragment_embeddings USING vec0(
			fragment_id TEXT PRIMARY KEY,
			embedding float[%d]
		);`, embeddingDim)
	if _, err := db.Exec(vtableSQL); err != nil {
		return fmt.Errorf("create vec0 virtual table: %w", err)
	}

	// Create the sqlite-vec virtual table for knowledge unit centroids.
	unitVtableSQL := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS unit_embeddings USING vec0(
			unit_id TEXT PRIMARY KEY,
			embedding float[%d]
		);`, embeddingDim)
	if _, err := db.Exec(unitVtableSQL); err != nil {
		return fmt.Errorf("create unit vec0 virtual table: %w", err)
	}

	// Create FTS5 full-text search table.
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS fragment_fts USING fts5(
		fragment_id UNINDEXED,
		content,
		tokenize='porter unicode61'
	)`); err != nil {
		return fmt.Errorf("create fts5 virtual table: %w", err)
	}

	// Backfill FTS index for existing databases upgrading to hybrid search.
	var ftsCount, fragCount int
	db.QueryRow("SELECT COUNT(*) FROM fragment_fts").Scan(&ftsCount)
	db.QueryRow("SELECT COUNT(*) FROM fragments").Scan(&fragCount)
	if ftsCount == 0 && fragCount > 0 {
		_, err := db.Exec(`INSERT INTO fragment_fts(fragment_id, content)
			SELECT id, COALESCE(enriched_content, raw_content) FROM fragments`)
		if err != nil {
			return fmt.Errorf("backfill fts: %w", err)
		}
	}

	// Validate or store embedding dimension in metadata.
	if err := validateOrSetMeta(db, "embedding_dim", fmt.Sprintf("%d", embeddingDim)); err != nil {
		return err
	}

	return nil
}

func validateOrSetMeta(db *sql.DB, key, expected string) error {
	// Use INSERT OR IGNORE to handle concurrent writers racing to set the
	// same key.  If the row already exists the INSERT is a no-op; we then
	// read back whatever value is stored and validate it.
	_, err := db.Exec("INSERT OR IGNORE INTO metadata (key, value) VALUES (?, ?)", key, expected)
	if err != nil {
		return fmt.Errorf("set metadata %s: %w", key, err)
	}

	var existing string
	if err := db.QueryRow("SELECT value FROM metadata WHERE key = ?", key).Scan(&existing); err != nil {
		return fmt.Errorf("read metadata %s: %w", key, err)
	}
	if existing != expected {
		return fmt.Errorf("metadata %s mismatch: db has %q, expected %q", key, existing, expected)
	}
	return nil
}

// isSQLiteBusy reports whether the error is a SQLite BUSY error.
func isSQLiteBusy(err error) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.ExtendedCode == sqlite3.ErrBusySnapshot
	}
	return false
}

// withRetry retries fn up to maxAttempts times when SQLite returns BUSY.
// It uses exponential backoff starting at 50ms.
func withRetry(maxAttempts int, fn func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		err = fn()
		if err == nil || !isSQLiteBusy(err) {
			return err
		}
		time.Sleep(time.Duration(50<<uint(attempt)) * time.Millisecond)
	}
	return err
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
// id, raw_content, enriched_content, source_type, source_name, source_path, source_uri,
// content_date, author, file_type, checksum, confidence_adj, ingested_at,
// enrichment_model, enrichment_version, embedding_model.
// Additional columns (e.g., distance, embedding) must be handled by the caller
// via the extra parameter.
func scanFragment(scanner interface{ Scan(...any) error }, extra ...any) (model.SourceFragment, error) {
	var f model.SourceFragment
	var contentDate, ingestedAt string
	var enrichedContent, enrichmentModel, enrichmentVersion, embeddingModel sql.NullString
	dest := []any{
		&f.ID, &f.RawContent, &enrichedContent, &f.SourceType, &f.SourceName, &f.SourcePath, &f.SourceURI,
		&contentDate, &f.Author, &f.FileType, &f.Checksum, &f.ConfidenceAdj, &ingestedAt,
		&enrichmentModel, &enrichmentVersion, &embeddingModel,
	}
	dest = append(dest, extra...)
	if err := scanner.Scan(dest...); err != nil {
		return model.SourceFragment{}, err
	}
	f.ContentDate, _ = time.Parse(time.RFC3339, contentDate)
	f.IngestedAt, _ = time.Parse(time.RFC3339, ingestedAt)
	f.EnrichedContent = enrichedContent.String
	f.EnrichmentModel = enrichmentModel.String
	f.EnrichmentVersion = enrichmentVersion.String
	f.EmbeddingModel = embeddingModel.String
	return f, nil
}

// UpsertFragments inserts or replaces fragments and their embeddings.
// It retries on SQLite BUSY errors to handle concurrent writers.
func (s *SQLiteStore) UpsertFragments(ctx context.Context, fragments []model.SourceFragment) error {
	return withRetry(3, func() error {
		return s.upsertFragmentsOnce(ctx, fragments)
	})
}

func (s *SQLiteStore) upsertFragmentsOnce(ctx context.Context, fragments []model.SourceFragment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	fragStmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO fragments
			(id, raw_content, enriched_content, source_type, source_name, source_path, source_uri, content_date, author, file_type, checksum, confidence_adj, enrichment_model, enrichment_version, embedding_model, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
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

	ftsDelStmt, err := tx.PrepareContext(ctx, `DELETE FROM fragment_fts WHERE fragment_id = ?`)
	if err != nil {
		return fmt.Errorf("prepare fts del stmt: %w", err)
	}
	defer ftsDelStmt.Close()

	ftsInsStmt, err := tx.PrepareContext(ctx, `INSERT INTO fragment_fts(fragment_id, content) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare fts ins stmt: %w", err)
	}
	defer ftsInsStmt.Close()

	for _, f := range fragments {
		var enrichedContent, enrichmentModel, enrichmentVersion, embeddingModel sql.NullString
		if f.EnrichedContent != "" {
			enrichedContent = sql.NullString{String: f.EnrichedContent, Valid: true}
		}
		if f.EnrichmentModel != "" {
			enrichmentModel = sql.NullString{String: f.EnrichmentModel, Valid: true}
		}
		if f.EnrichmentVersion != "" {
			enrichmentVersion = sql.NullString{String: f.EnrichmentVersion, Valid: true}
		}
		if f.EmbeddingModel != "" {
			embeddingModel = sql.NullString{String: f.EmbeddingModel, Valid: true}
		}
		_, err := fragStmt.ExecContext(ctx,
			f.ID, f.RawContent, enrichedContent, f.SourceType, f.SourceName, f.SourcePath, f.SourceURI,
			f.ContentDate.UTC().Format(time.RFC3339),
			f.Author, f.FileType, f.Checksum, f.ConfidenceAdj,
			enrichmentModel, enrichmentVersion, embeddingModel,
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

		// Sync FTS index.
		ftsContent := f.RawContent
		if f.EnrichedContent != "" {
			ftsContent = f.EnrichedContent
		}
		if _, err = ftsDelStmt.ExecContext(ctx, f.ID); err != nil {
			return fmt.Errorf("delete fts %s: %w", f.ID, err)
		}
		if _, err = ftsInsStmt.ExecContext(ctx, f.ID, ftsContent); err != nil {
			return fmt.Errorf("insert fts %s: %w", f.ID, err)
		}
	}

	return tx.Commit()
}

// SearchByVector finds the nearest fragments to the given embedding vector.
func (s *SQLiteStore) SearchByVector(ctx context.Context, embedding []float32, limit int) ([]model.SourceFragment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.raw_content, f.enriched_content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.content_date, f.author, f.file_type, f.checksum, f.confidence_adj, f.ingested_at,
		       f.enrichment_model, f.enrichment_version, f.embedding_model,
		       fe.distance
		FROM fragment_embeddings fe
		INNER JOIN fragments f ON f.id = fe.fragment_id
		WHERE fe.embedding MATCH ? AND k = ?
		ORDER BY fe.distance, f.id
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

// SearchByVectorFiltered finds the nearest fragments filtered by source names and/or types.
// If both sourceNames and sourceTypes are empty, it delegates to SearchByVector.
// Since sqlite-vec doesn't support WHERE clauses on joined columns in the
// virtual table query, we over-fetch from the vector index and post-filter.
func (s *SQLiteStore) SearchByVectorFiltered(ctx context.Context, embedding []float32, limit int, sourceNames []string, sourceTypes []string) ([]model.SourceFragment, error) {
	if len(sourceNames) == 0 && len(sourceTypes) == 0 {
		return s.SearchByVector(ctx, embedding, limit)
	}

	// Over-fetch to account for filtering. We fetch limit*5 candidates from
	// the vector index and then filter by source_name/source_type.
	overFetch := limit * 5
	if overFetch < 50 {
		overFetch = 50
	}

	filterSQL, filterArgs := buildSourceFilters(sourceNames, sourceTypes)
	args := []interface{}{serializeEmbedding(embedding), overFetch}
	args = append(args, filterArgs...)

	query := fmt.Sprintf(`
		SELECT f.id, f.raw_content, f.enriched_content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.content_date, f.author, f.file_type, f.checksum, f.confidence_adj, f.ingested_at,
		       f.enrichment_model, f.enrichment_version, f.embedding_model,
		       fe.distance
		FROM fragment_embeddings fe
		INNER JOIN fragments f ON f.id = fe.fragment_id
		WHERE fe.embedding MATCH ? AND k = ?
		  AND %s
		ORDER BY fe.distance, f.id
		LIMIT ?
	`, filterSQL)
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

// sanitizeFTSQuery converts a natural language query into a safe FTS5 MATCH
// expression. It quotes each word to treat special characters as literals,
// then joins with implicit AND.
func sanitizeFTSQuery(query string) string {
	words := strings.Fields(query)
	var quoted []string
	for _, w := range words {
		// Remove characters that break FTS5 even inside quotes.
		clean := strings.Map(func(r rune) rune {
			if r == '"' {
				return -1
			}
			return r
		}, w)
		if clean != "" {
			quoted = append(quoted, `"`+clean+`"`)
		}
	}
	return strings.Join(quoted, " ")
}

// SearchByFTS performs full-text keyword search using BM25 ranking.
func (s *SQLiteStore) SearchByFTS(ctx context.Context, query string, limit int) ([]model.SourceFragment, error) {
	sanitized := sanitizeFTSQuery(query)
	if sanitized == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.raw_content, f.enriched_content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.content_date, f.author, f.file_type, f.checksum, f.confidence_adj, f.ingested_at,
		       f.enrichment_model, f.enrichment_version, f.embedding_model,
		       fts.rank
		FROM fragment_fts fts
		INNER JOIN fragments f ON f.id = fts.fragment_id
		WHERE fragment_fts MATCH ?
		ORDER BY fts.rank, f.id
		LIMIT ?
	`, sanitized, limit)
	if err != nil {
		return nil, fmt.Errorf("search by fts: %w", err)
	}
	defer rows.Close()

	var results []model.SourceFragment
	for rows.Next() {
		var rank float64
		f, err := scanFragment(rows, &rank)
		if err != nil {
			return nil, fmt.Errorf("scan fts fragment: %w", err)
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// buildSourceFilters constructs SQL WHERE conditions and args for source name/type filtering.
func buildSourceFilters(sourceNames, sourceTypes []string) (string, []interface{}) {
	var conditions []string
	var args []interface{}
	if len(sourceNames) > 0 {
		placeholders := make([]string, len(sourceNames))
		for i, n := range sourceNames {
			placeholders[i] = "?"
			args = append(args, n)
		}
		conditions = append(conditions, fmt.Sprintf("f.source_name IN (%s)", strings.Join(placeholders, ",")))
	}
	if len(sourceTypes) > 0 {
		placeholders := make([]string, len(sourceTypes))
		for i, t := range sourceTypes {
			placeholders[i] = "?"
			args = append(args, t)
		}
		conditions = append(conditions, fmt.Sprintf("f.source_type IN (%s)", strings.Join(placeholders, ",")))
	}
	return strings.Join(conditions, " AND "), args
}

// SearchByFTSFiltered performs full-text search filtered by source names and/or types.
func (s *SQLiteStore) SearchByFTSFiltered(ctx context.Context, query string, limit int, sourceNames []string, sourceTypes []string) ([]model.SourceFragment, error) {
	if len(sourceNames) == 0 && len(sourceTypes) == 0 {
		return s.SearchByFTS(ctx, query, limit)
	}

	sanitized := sanitizeFTSQuery(query)
	if sanitized == "" {
		return nil, nil
	}
	filterSQL, filterArgs := buildSourceFilters(sourceNames, sourceTypes)
	args := []interface{}{sanitized}
	args = append(args, filterArgs...)
	args = append(args, limit)

	q := fmt.Sprintf(`
		SELECT f.id, f.raw_content, f.enriched_content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.content_date, f.author, f.file_type, f.checksum, f.confidence_adj, f.ingested_at,
		       f.enrichment_model, f.enrichment_version, f.embedding_model,
		       fts.rank
		FROM fragment_fts fts
		INNER JOIN fragments f ON f.id = fts.fragment_id
		WHERE fragment_fts MATCH ? AND %s
		ORDER BY fts.rank, f.id
		LIMIT ?
	`, filterSQL)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search by fts filtered: %w", err)
	}
	defer rows.Close()

	var results []model.SourceFragment
	for rows.Next() {
		var rank float64
		f, err := scanFragment(rows, &rank)
		if err != nil {
			return nil, fmt.Errorf("scan fts fragment: %w", err)
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
		SELECT id, raw_content, enriched_content, source_type, source_name, source_path, source_uri,
		       content_date, author, file_type, checksum, confidence_adj, ingested_at,
		       enrichment_model, enrichment_version, embedding_model
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
// It retries on SQLite BUSY errors to handle concurrent writers.
func (s *SQLiteStore) DeleteByPaths(ctx context.Context, sourceType, sourceName string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	return withRetry(3, func() error {
		return s.deleteByPathsOnce(ctx, sourceType, sourceName, paths)
	})
}

func (s *SQLiteStore) deleteByPathsOnce(ctx context.Context, sourceType, sourceName string, paths []string) error {
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

	// Delete from fragment_fts first (referencing fragment IDs).
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`
		DELETE FROM fragment_fts
		WHERE fragment_id IN (
			SELECT id FROM fragments WHERE source_type = ?%s AND source_path IN (%s)
		)
	`, nameFilter, inClause), args...)
	if err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}

	// Delete from fragment_embeddings (referencing fragment IDs).
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
		SELECT f.id, f.raw_content, f.enriched_content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.content_date, f.author, f.file_type, f.checksum, f.confidence_adj, f.ingested_at,
		       f.enrichment_model, f.enrichment_version, f.embedding_model,
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

// GetFragmentsBySource returns all fragments for a given source name, with embeddings.
func (s *SQLiteStore) GetFragmentsBySource(ctx context.Context, sourceName string) ([]model.SourceFragment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT f.id, f.raw_content, f.enriched_content, f.source_type, f.source_name, f.source_path, f.source_uri,
		       f.content_date, f.author, f.file_type, f.checksum, f.confidence_adj, f.ingested_at,
		       f.enrichment_model, f.enrichment_version, f.embedding_model,
		       fe.embedding
		FROM fragments f
		INNER JOIN fragment_embeddings fe ON fe.fragment_id = f.id
		WHERE f.source_name = ?
		ORDER BY f.source_path, f.id
	`, sourceName)
	if err != nil {
		return nil, fmt.Errorf("get fragments by source: %w", err)
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

// formatTimePtr formats a *time.Time as RFC3339 UTC, or nil (SQL NULL) if unset.
func formatTimePtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

// RegisterSource inserts or updates a registered source.
func (s *SQLiteStore) RegisterSource(ctx context.Context, src model.Source) error {
	configJSON, err := json.Marshal(src.Config)
	if err != nil {
		return fmt.Errorf("marshal source config: %w", err)
	}
	if src.Description != "" {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO sources (source_type, source_name, config, last_ingest, description)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(source_type, source_name) DO UPDATE SET
				config = excluded.config,
				last_ingest = excluded.last_ingest,
				description = excluded.description
		`, src.SourceType, src.SourceName, string(configJSON), formatTimePtr(src.LastIngest), src.Description)
	} else {
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO sources (source_type, source_name, config, last_ingest)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(source_type, source_name) DO UPDATE SET
				config = excluded.config,
				last_ingest = excluded.last_ingest
		`, src.SourceType, src.SourceName, string(configJSON), formatTimePtr(src.LastIngest))
	}
	if err != nil {
		return fmt.Errorf("register source: %w", err)
	}
	return nil
}

// ListSources returns all registered sources.
func (s *SQLiteStore) ListSources(ctx context.Context) ([]model.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT source_type, source_name, config, last_ingest, description FROM sources ORDER BY source_type, source_name")
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	defer rows.Close()

	var sources []model.Source
	for rows.Next() {
		var src model.Source
		var configJSON string
		var lastIngest sql.NullString
		if err := rows.Scan(&src.SourceType, &src.SourceName, &configJSON, &lastIngest, &src.Description); err != nil {
			return nil, fmt.Errorf("scan source: %w", err)
		}
		if err := json.Unmarshal([]byte(configJSON), &src.Config); err != nil {
			return nil, fmt.Errorf("unmarshal source config: %w", err)
		}
		if lastIngest.Valid {
			if t, err := time.Parse(time.RFC3339, lastIngest.String); err == nil {
				src.LastIngest = &t
			}
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

// ContentSizeBySource returns a map of "source_type/source_name" to total content size in bytes.
func (s *SQLiteStore) ContentSizeBySource(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT source_type || '/' || source_name AS key, SUM(LENGTH(raw_content)) FROM fragments GROUP BY source_type, source_name`)
	if err != nil {
		return nil, fmt.Errorf("content size by source: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int64)
	for rows.Next() {
		var key string
		var size int64
		if err := rows.Scan(&key, &size); err != nil {
			return nil, fmt.Errorf("scan size: %w", err)
		}
		result[key] = size
	}
	return result, rows.Err()
}

// DeleteFragmentsBySource removes all fragments and their embeddings for the given source type and name.
func (s *SQLiteStore) DeleteFragmentsBySource(ctx context.Context, sourceType, sourceName string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete from fragment_fts first (referencing fragment IDs).
	_, err = tx.ExecContext(ctx, `
		DELETE FROM fragment_fts
		WHERE fragment_id IN (
			SELECT id FROM fragments WHERE source_type = ? AND source_name = ?
		)
	`, sourceType, sourceName)
	if err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}

	// Delete from fragment_embeddings (referencing fragment IDs).
	_, err = tx.ExecContext(ctx, `
		DELETE FROM fragment_embeddings
		WHERE fragment_id IN (
			SELECT id FROM fragments WHERE source_type = ? AND source_name = ?
		)
	`, sourceType, sourceName)
	if err != nil {
		return fmt.Errorf("delete embeddings: %w", err)
	}

	// Delete from fragments.
	_, err = tx.ExecContext(ctx, `
		DELETE FROM fragments WHERE source_type = ? AND source_name = ?
	`, sourceType, sourceName)
	if err != nil {
		return fmt.Errorf("delete fragments: %w", err)
	}

	return tx.Commit()
}

// GetSource retrieves a single source by type and name. Returns nil if not found.
func (s *SQLiteStore) GetSource(ctx context.Context, sourceType, sourceName string) (*model.Source, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT source_type, source_name, config, last_ingest, description FROM sources WHERE source_type = ? AND source_name = ?",
		sourceType, sourceName)

	var src model.Source
	var configJSON string
	var lastIngest sql.NullString
	if err := row.Scan(&src.SourceType, &src.SourceName, &configJSON, &lastIngest, &src.Description); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get source: %w", err)
	}
	if err := json.Unmarshal([]byte(configJSON), &src.Config); err != nil {
		return nil, fmt.Errorf("unmarshal source config: %w", err)
	}
	if lastIngest.Valid {
		if t, err := time.Parse(time.RFC3339, lastIngest.String); err == nil {
			src.LastIngest = &t
		}
	}
	return &src, nil
}

// UpdateSourceDescription sets the description for an existing source.
// If force is false and the source already has a non-empty description, it returns an error.
func (s *SQLiteStore) UpdateSourceDescription(ctx context.Context, sourceType, sourceName, description string, force bool) error {
	if !force {
		existing, err := s.GetSource(ctx, sourceType, sourceName)
		if err != nil {
			return err
		}
		if existing != nil && existing.Description != "" {
			return fmt.Errorf("source %s/%s already has a description (use --force to overwrite)", sourceType, sourceName)
		}
	}

	res, err := s.db.ExecContext(ctx, `UPDATE sources SET description = ? WHERE source_type = ? AND source_name = ?`, description, sourceType, sourceName)
	if err != nil {
		return fmt.Errorf("update source description: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("source %s/%s not found", sourceType, sourceName)
	}
	return nil
}

// DeleteSource removes a source registration from the sources table.
func (s *SQLiteStore) DeleteSource(ctx context.Context, sourceType, sourceName string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM sources WHERE source_type = ? AND source_name = ?
	`, sourceType, sourceName)
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}
	return nil
}

// UpsertKnowledgeUnit inserts or replaces a knowledge unit, its fragment
// associations, and its centroid embedding.
func (s *SQLiteStore) UpsertKnowledgeUnit(ctx context.Context, unit model.KnowledgeUnit) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO knowledge_units
			(id, topic, summary, confidence_freshness, confidence_corroboration,
			 confidence_consistency, confidence_authority, last_computed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, unit.ID, unit.Topic, unit.Summary,
		unit.Confidence.Breakdown.Freshness, unit.Confidence.Breakdown.Corroboration,
		unit.Confidence.Breakdown.Consistency, unit.Confidence.Breakdown.Authority,
		unit.LastComputed.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("upsert knowledge unit: %w", err)
	}

	// Replace fragment associations.
	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_unit_fragments WHERE unit_id = ?", unit.ID); err != nil {
		return fmt.Errorf("delete unit fragments: %w", err)
	}
	if len(unit.FragmentIDs) > 0 {
		stmt, err := tx.PrepareContext(ctx, "INSERT INTO knowledge_unit_fragments (unit_id, fragment_id) VALUES (?, ?)")
		if err != nil {
			return fmt.Errorf("prepare unit fragment stmt: %w", err)
		}
		defer stmt.Close()
		for _, fid := range unit.FragmentIDs {
			if _, err := stmt.ExecContext(ctx, unit.ID, fid); err != nil {
				return fmt.Errorf("insert unit fragment: %w", err)
			}
		}
	}

	// Upsert centroid embedding.
	if len(unit.Centroid) > 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM unit_embeddings WHERE unit_id = ?", unit.ID); err != nil {
			return fmt.Errorf("delete old unit embedding: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO unit_embeddings (unit_id, embedding) VALUES (?, ?)",
			unit.ID, serializeEmbedding(unit.Centroid)); err != nil {
			return fmt.Errorf("insert unit embedding: %w", err)
		}
	}

	return tx.Commit()
}
// ListKnowledgeUnits returns all knowledge units with their fragment IDs.
func (s *SQLiteStore) ListKnowledgeUnits(ctx context.Context) ([]model.KnowledgeUnit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, topic, summary,
		       confidence_freshness, confidence_corroboration,
		       confidence_consistency, confidence_authority,
		       last_computed
		FROM knowledge_units
		ORDER BY topic
	`)
	if err != nil {
		return nil, fmt.Errorf("list knowledge units: %w", err)
	}
	defer rows.Close()

	var units []model.KnowledgeUnit
	for rows.Next() {
		u, err := scanKnowledgeUnit(rows)
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load fragment IDs for each unit.
	for i := range units {
		fids, err := s.loadUnitFragmentIDs(ctx, units[i].ID)
		if err != nil {
			return nil, err
		}
		units[i].FragmentIDs = fids
	}

	return units, nil
}

// GetKnowledgeUnit retrieves a single knowledge unit by ID.
func (s *SQLiteStore) GetKnowledgeUnit(ctx context.Context, id string) (*model.KnowledgeUnit, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, topic, summary,
		       confidence_freshness, confidence_corroboration,
		       confidence_consistency, confidence_authority,
		       last_computed
		FROM knowledge_units WHERE id = ?
	`, id)

	u, err := scanKnowledgeUnit(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get knowledge unit: %w", err)
	}

	u.FragmentIDs, err = s.loadUnitFragmentIDs(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return &u, nil
}

// SearchKnowledgeUnits finds the nearest knowledge units by centroid embedding.
func (s *SQLiteStore) SearchKnowledgeUnits(ctx context.Context, embedding []float32, limit int) ([]model.KnowledgeUnit, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ku.id, ku.topic, ku.summary,
		       ku.confidence_freshness, ku.confidence_corroboration,
		       ku.confidence_consistency, ku.confidence_authority,
		       ku.last_computed, ue.distance
		FROM unit_embeddings ue
		INNER JOIN knowledge_units ku ON ku.id = ue.unit_id
		WHERE ue.embedding MATCH ? AND k = ?
		ORDER BY ue.distance
	`, serializeEmbedding(embedding), limit)
	if err != nil {
		return nil, fmt.Errorf("search knowledge units: %w", err)
	}
	defer rows.Close()

	var units []model.KnowledgeUnit
	for rows.Next() {
		var distance float64
		u, err := scanKnowledgeUnitExtra(rows, &distance)
		if err != nil {
			return nil, err
		}
		units = append(units, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load fragment IDs for each unit.
	for i := range units {
		fids, err := s.loadUnitFragmentIDs(ctx, units[i].ID)
		if err != nil {
			return nil, err
		}
		units[i].FragmentIDs = fids
	}

	return units, nil
}

// DeleteAllKnowledgeUnits removes all knowledge units, their fragment
// associations, and their centroid embeddings.
func (s *SQLiteStore) DeleteAllKnowledgeUnits(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_unit_fragments"); err != nil {
		return fmt.Errorf("delete unit fragments: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM unit_embeddings"); err != nil {
		return fmt.Errorf("delete unit embeddings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM knowledge_units"); err != nil {
		return fmt.Errorf("delete knowledge units: %w", err)
	}

	return tx.Commit()
}

// scanKnowledgeUnit scans a knowledge unit row (without extra columns).
func scanKnowledgeUnit(scanner interface{ Scan(...any) error }) (model.KnowledgeUnit, error) {
	return scanKnowledgeUnitExtra(scanner)
}

// scanKnowledgeUnitExtra scans a knowledge unit row with optional extra columns.
func scanKnowledgeUnitExtra(scanner interface{ Scan(...any) error }, extra ...any) (model.KnowledgeUnit, error) {
	var u model.KnowledgeUnit
	var lastComputed string
	dest := []any{
		&u.ID, &u.Topic, &u.Summary,
		&u.Confidence.Breakdown.Freshness, &u.Confidence.Breakdown.Corroboration,
		&u.Confidence.Breakdown.Consistency, &u.Confidence.Breakdown.Authority,
		&lastComputed,
	}
	dest = append(dest, extra...)
	if err := scanner.Scan(dest...); err != nil {
		return model.KnowledgeUnit{}, err
	}
	u.LastComputed, _ = time.Parse(time.RFC3339, lastComputed)
	return u, nil
}

// loadUnitFragmentIDs returns fragment IDs associated with a knowledge unit.
func (s *SQLiteStore) loadUnitFragmentIDs(ctx context.Context, unitID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT fragment_id FROM knowledge_unit_fragments WHERE unit_id = ? ORDER BY fragment_id", unitID)
	if err != nil {
		return nil, fmt.Errorf("load unit fragment ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan fragment id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
