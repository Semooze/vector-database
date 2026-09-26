package lab

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Dimension() int { return 3 }
func (fakeEmbedder) Documents(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1, 0, 0}
	}
	return out, nil
}
func (fakeEmbedder) Query(_ context.Context, text string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

type countingEmbedder struct{ queries int }

func (c *countingEmbedder) Dimension() int { return 3 }
func (c *countingEmbedder) Documents(ctx context.Context, texts []string) ([][]float32, error) {
	return fakeEmbedder{}.Documents(ctx, texts)
}
func (c *countingEmbedder) Query(ctx context.Context, text string) ([]float32, error) {
	c.queries++
	return fakeEmbedder{}.Query(ctx, text)
}

type batchEmbedder struct{ batches []int }

func (b *batchEmbedder) Dimension() int { return 3 }
func (b *batchEmbedder) Documents(ctx context.Context, texts []string) ([][]float32, error) {
	b.batches = append(b.batches, len(texts))
	return fakeEmbedder{}.Documents(ctx, texts)
}
func (b *batchEmbedder) Query(ctx context.Context, text string) ([]float32, error) {
	return fakeEmbedder{}.Query(ctx, text)
}

type fakeStore struct{ chunks []Chunk }

func (s *fakeStore) Ready(context.Context) error { return nil }
func (s *fakeStore) Upsert(_ context.Context, _ string, _ int, chunks []Chunk) error {
	s.chunks = chunks
	return nil
}
func (s *fakeStore) Search(_ context.Context, _ string, _ []float32, _ int) ([]Hit, error) {
	return []Hit{{DocumentID: s.chunks[0].DocumentID, Title: s.chunks[0].Title, Text: s.chunks[0].Text, Score: 1}}, nil
}

func TestServiceIndexesAndSearchesSelectedPairs(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "lab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	repo.SaveCorpus(Corpus{ID: "c", Documents: []Document{{ID: "d", Title: "Trail", Text: "shaded forest trail"}}})
	store := &fakeStore{}
	svc := NewService(repo, map[string]Embedder{"minilm": fakeEmbedder{}, "qwen": fakeEmbedder{}}, map[string]VectorStore{"postgres": store, "mongo": store})
	run, err := svc.CreateRun("c", []Pair{{Store: "postgres", Model: "minilm"}, {Store: "mongo", Model: "qwen"}})
	if err != nil {
		t.Fatal(err)
	}
	svc.IndexRun(context.Background(), run.ID)
	got, err := svc.Search(context.Background(), run.ID, "trees")
	if err != nil || len(got.Results) != 2 || got.Results[0].Hits[0].DocumentID != "d" {
		t.Fatalf("search=%+v err=%v", got, err)
	}
}

func TestEvaluationPersistsWithRun(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "lab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	repo.SaveCorpus(Corpus{ID: "sample", Sample: true, Documents: []Document{{ID: "d", Title: "Trail", Text: "shaded forest"}}, Questions: []Question{{Query: "shade", RelevantIDs: []string{"d"}}}})
	store := &fakeStore{}
	svc := NewService(repo, map[string]Embedder{"minilm": fakeEmbedder{}}, map[string]VectorStore{"postgres": store, "mongo": store})
	run, err := svc.CreateRun("sample", []Pair{{Store: "postgres", Model: "minilm"}, {Store: "mongo", Model: "minilm"}})
	if err != nil {
		t.Fatal(err)
	}
	svc.IndexRun(context.Background(), run.ID)
	rows, err := svc.Evaluate(context.Background(), run.ID)
	if err != nil || len(rows) != 2 || rows[0].RecallAt5 != 1 {
		t.Fatalf("evaluation=%v err=%v", rows, err)
	}
	loaded, err := repo.GetRun(run.ID)
	if err != nil || len(loaded.LatestEvaluation) != 2 {
		t.Fatalf("saved evaluation=%v err=%v", loaded.LatestEvaluation, err)
	}
}

func TestMedianAveragesMiddleTwoValues(t *testing.T) {
	if got := median([]int64{9, 1, 3, 7}); got != 5 {
		t.Fatalf("median=%d", got)
	}
}

func TestEvaluationEmbedsOneQueryPerModelAcrossStores(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "lab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	repo.SaveCorpus(Corpus{ID: "sample", Sample: true, Documents: []Document{{ID: "d", Title: "Trail", Text: "shaded forest"}}, Questions: []Question{{Query: "shade", RelevantIDs: []string{"d"}}}})
	store := &fakeStore{}
	embedder := &countingEmbedder{}
	svc := NewService(repo, map[string]Embedder{"minilm": embedder}, map[string]VectorStore{"postgres": store, "mongo": store})
	run, err := svc.CreateRun("sample", []Pair{{Store: "postgres", Model: "minilm"}, {Store: "mongo", Model: "minilm"}})
	if err != nil {
		t.Fatal(err)
	}
	svc.IndexRun(context.Background(), run.ID)
	if _, err := svc.Evaluate(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if embedder.queries != 1 {
		t.Fatalf("query embedding calls=%d", embedder.queries)
	}
}

func TestIndexRunBatchesLargeCorpusEmbedding(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "lab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	repo.SaveCorpus(Corpus{ID: "large", Documents: []Document{{ID: "d", Title: "Trail", Text: strings.Repeat("tree ", 20500)}}})
	embedder := &batchEmbedder{}
	store := &fakeStore{}
	svc := NewService(repo, map[string]Embedder{"minilm": embedder}, map[string]VectorStore{"postgres": store, "mongo": store})
	run, err := svc.CreateRun("large", []Pair{{Store: "postgres", Model: "minilm"}, {Store: "mongo", Model: "minilm"}})
	if err != nil {
		t.Fatal(err)
	}
	svc.IndexRun(context.Background(), run.ID)
	if len(embedder.batches) < 2 {
		t.Fatalf("embedding batches=%v", embedder.batches)
	}
	for _, size := range embedder.batches {
		if size > 16 {
			t.Fatalf("batch too large: %d", size)
		}
	}
}
