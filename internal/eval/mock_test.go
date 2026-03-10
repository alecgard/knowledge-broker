package eval

import (
	"context"
	"time"

	"github.com/knowledge-broker/knowledge-broker/pkg/model"
)

// mockFragment holds test fragment data.
type mockFragment struct {
	id         string
	sourcePath string
	content    string
}

// mockStore implements store.Store for testing.
type mockStore struct {
	fragments []mockFragment
}

func (m *mockStore) UpsertFragments(ctx context.Context, fragments []model.SourceFragment) error {
	return nil
}

func (m *mockStore) SearchByVector(ctx context.Context, embedding []float32, limit int) ([]model.SourceFragment, error) {
	var results []model.SourceFragment
	for i, f := range m.fragments {
		if i >= limit {
			break
		}
		results = append(results, model.SourceFragment{
			ID:         f.id,
			SourcePath: f.sourcePath,
			Content:    f.content,
		})
	}
	return results, nil
}

func (m *mockStore) SearchByVectorFiltered(ctx context.Context, embedding []float32, limit int, sourceNames []string) ([]model.SourceFragment, error) {
	return m.SearchByVector(ctx, embedding, limit)
}

func (m *mockStore) GetFragments(ctx context.Context, ids []string) ([]model.SourceFragment, error) {
	return nil, nil
}

func (m *mockStore) GetChecksums(ctx context.Context, sourceType, sourceName string) (map[string]string, error) {
	return nil, nil
}

func (m *mockStore) DeleteByPaths(ctx context.Context, sourceType, sourceName string, paths []string) error {
	return nil
}

func (m *mockStore) ExportFragments(ctx context.Context) ([]model.SourceFragment, error) {
	var results []model.SourceFragment
	for _, f := range m.fragments {
		results = append(results, model.SourceFragment{
			ID:           f.id,
			SourcePath:   f.sourcePath,
			Content:      f.content,
			LastModified: time.Now(),
			Embedding:    []float32{0.1, 0.2, 0.3, 0.4},
		})
	}
	return results, nil
}

func (m *mockStore) RegisterSource(ctx context.Context, src model.Source) error {
	return nil
}

func (m *mockStore) ListSources(ctx context.Context) ([]model.Source, error) {
	return nil, nil
}

func (m *mockStore) CountFragmentsBySource(ctx context.Context) (map[string]int, error) {
	return nil, nil
}

func (m *mockStore) DeleteFragmentsBySource(ctx context.Context, sourceType, sourceName string) error {
	return nil
}

func (m *mockStore) DeleteSource(ctx context.Context, sourceType, sourceName string) error {
	return nil
}

func (m *mockStore) UpsertKnowledgeUnit(ctx context.Context, unit model.KnowledgeUnit) error {
	return nil
}

func (m *mockStore) ListKnowledgeUnits(ctx context.Context) ([]model.KnowledgeUnit, error) {
	return nil, nil
}

func (m *mockStore) GetKnowledgeUnit(ctx context.Context, id string) (*model.KnowledgeUnit, error) {
	return nil, nil
}

func (m *mockStore) SearchKnowledgeUnits(ctx context.Context, embedding []float32, limit int) ([]model.KnowledgeUnit, error) {
	return nil, nil
}

func (m *mockStore) DeleteAllKnowledgeUnits(ctx context.Context) error {
	return nil
}

func (m *mockStore) Close() error {
	return nil
}

// mockEmbedder implements embedding.Embedder for testing.
type mockEmbedder struct {
	dim int
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	v := make([]float32, m.dim)
	for i := range v {
		v[i] = 0.1
	}
	return v, nil
}

func (m *mockEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	results := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, m.dim)
		for j := range v {
			v[j] = 0.1
		}
		results[i] = v
	}
	return results, nil
}

func (m *mockEmbedder) Dimension() int {
	return m.dim
}

func (m *mockEmbedder) CheckHealth(_ context.Context) error {
	return nil
}
