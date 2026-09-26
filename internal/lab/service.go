package lab

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Embedder interface {
	Dimension() int
	Documents(context.Context, []string) ([][]float32, error)
	Query(context.Context, string) ([]float32, error)
}

type VectorStore interface {
	Ready(context.Context) error
	Upsert(context.Context, string, int, []Chunk) error
	Search(context.Context, string, []float32, int) ([]Hit, error)
}

type Service struct {
	repo      *Repository
	embedders map[string]Embedder
	stores    map[string]VectorStore
	mu        sync.Mutex
}

func NewService(repo *Repository, embedders map[string]Embedder, stores map[string]VectorStore) *Service {
	return &Service{repo: repo, embedders: embedders, stores: stores}
}

func newID() string { b := make([]byte, 12); _, _ = rand.Read(b); return hex.EncodeToString(b) }

func (s *Service) CreateRun(corpusID string, pairs []Pair) (Run, error) {
	if err := ValidatePairs(pairs); err != nil {
		return Run{}, err
	}
	if _, err := s.repo.GetCorpus(corpusID); err != nil {
		return Run{}, fmt.Errorf("corpus: %w", err)
	}
	run := Run{ID: newID(), CorpusID: corpusID, CreatedAt: time.Now().UTC(), Pairs: make([]PairRun, len(pairs))}
	for i, p := range pairs {
		run.Pairs[i] = PairRun{Pair: p, Status: "queued"}
	}
	return run, s.repo.SaveRun(run)
}

func resourceName(runID, model string) string { return "vl_" + runID + "_" + model }

func (s *Service) setPair(runID string, index int, status string, indexMS int64, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, err := s.repo.GetRun(runID)
	if err != nil {
		return
	}
	run.Pairs[index].Status = status
	run.Pairs[index].IndexMS = indexMS
	run.Pairs[index].Error = reason
	_ = s.repo.SaveRun(run)
}

func (s *Service) IndexRun(ctx context.Context, runID string) {
	run, err := s.repo.GetRun(runID)
	if err != nil {
		return
	}
	corpus, err := s.repo.GetCorpus(run.CorpusID)
	if err != nil {
		return
	}
	chunks := make([]Chunk, 0)
	for _, doc := range corpus.Documents {
		for i, part := range ChunkText(doc.Text, 100, 20) {
			chunks = append(chunks, Chunk{ID: fmt.Sprintf("%s-%d", doc.ID, i), DocumentID: doc.ID, Title: doc.Title, Text: part})
		}
	}
	if len(chunks) == 0 {
		for i := range run.Pairs {
			s.setPair(runID, i, "failed", 0, "corpus has no text")
		}
		return
	}
	cache := map[string][][]float32{}
	modelErrors := map[string]error{}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	for i, p := range run.Pairs {
		if p.Status != "queued" {
			continue
		}
		s.setPair(runID, i, "indexing", 0, "")
		embedder := s.embedders[p.Pair.Model]
		store := s.stores[p.Pair.Store]
		if embedder == nil || store == nil {
			s.setPair(runID, i, "failed", 0, "provider not configured")
			continue
		}
		if _, ok := cache[p.Pair.Model]; !ok && modelErrors[p.Pair.Model] == nil {
			cache[p.Pair.Model], modelErrors[p.Pair.Model] = embedDocumentsInBatches(ctx, embedder, texts)
		}
		if err := modelErrors[p.Pair.Model]; err != nil {
			s.setPair(runID, i, "failed", 0, err.Error())
			continue
		}
		vectors := cache[p.Pair.Model]
		if len(vectors) != len(chunks) {
			s.setPair(runID, i, "failed", 0, "embedding count mismatch")
			continue
		}
		indexed := make([]Chunk, len(chunks))
		copy(indexed, chunks)
		valid := true
		for j := range indexed {
			if len(vectors[j]) != embedder.Dimension() {
				valid = false
				break
			}
			indexed[j].Vector = vectors[j]
		}
		if !valid {
			s.setPair(runID, i, "failed", 0, "embedding dimension mismatch")
			continue
		}
		start := time.Now()
		err := store.Upsert(ctx, resourceName(runID, p.Pair.Model), embedder.Dimension(), indexed)
		ms := time.Since(start).Milliseconds()
		if err != nil {
			s.setPair(runID, i, "failed", ms, err.Error())
			continue
		}
		s.setPair(runID, i, "ready", ms, "")
	}
}

func embedDocumentsInBatches(ctx context.Context, embedder Embedder, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += 16 {
		end := start + 16
		if end > len(texts) {
			end = len(texts)
		}
		batch, err := embedder.Documents(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if len(batch) != end-start {
			return nil, errors.New("embedding count mismatch")
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (s *Service) Search(ctx context.Context, runID, query string) (SearchResponse, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 1000 {
		return SearchResponse{}, errors.New("query must be 1 to 1000 characters")
	}
	run, err := s.repo.GetRun(runID)
	if err != nil {
		return SearchResponse{}, err
	}
	response := SearchResponse{Query: query, Results: []PairResult{}}
	cache := map[string][]float32{}
	embedTimes := map[string]int64{}
	modelErrors := map[string]error{}
	for _, p := range run.Pairs {
		if p.Status != "ready" {
			continue
		}
		result := PairResult{Pair: p.Pair, Hits: []Hit{}}
		embedder := s.embedders[p.Pair.Model]
		store := s.stores[p.Pair.Store]
		if embedder == nil || store == nil {
			result.Error = "provider not configured"
			response.Results = append(response.Results, result)
			continue
		}
		if _, ok := cache[p.Pair.Model]; !ok && modelErrors[p.Pair.Model] == nil {
			start := time.Now()
			cache[p.Pair.Model], modelErrors[p.Pair.Model] = embedder.Query(ctx, query)
			embedTimes[p.Pair.Model] = time.Since(start).Milliseconds()
		}
		result.EmbedMS = embedTimes[p.Pair.Model]
		if err := modelErrors[p.Pair.Model]; err != nil {
			result.Error = err.Error()
			response.Results = append(response.Results, result)
			continue
		}
		if len(cache[p.Pair.Model]) != embedder.Dimension() {
			result.Error = "embedding dimension mismatch"
			response.Results = append(response.Results, result)
			continue
		}
		start := time.Now()
		result.Hits, err = store.Search(ctx, resourceName(runID, p.Pair.Model), cache[p.Pair.Model], 5)
		result.SearchMS = time.Since(start).Milliseconds()
		if err != nil {
			result.Error = err.Error()
		}
		response.Results = append(response.Results, result)
	}
	if len(response.Results) == 0 {
		return response, errors.New("no ready pairs")
	}
	s.mu.Lock()
	run, err = s.repo.GetRun(runID)
	if err == nil {
		run.Latest = &response
		_ = s.repo.SaveRun(run)
	}
	s.mu.Unlock()
	return response, nil
}

type Evaluation struct {
	Pair           Pair    `json:"pair"`
	RecallAt5      float64 `json:"recall_at_5"`
	MRRAt5         float64 `json:"mrr_at_5"`
	MedianEmbedMS  int64   `json:"median_embed_ms"`
	MedianSearchMS int64   `json:"median_search_ms"`
	IndexMS        int64   `json:"index_ms"`
	Queries        int     `json:"queries"`
	Error          string  `json:"error,omitempty"`
}

func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
	if len(values)%2 == 0 {
		return (values[len(values)/2-1] + values[len(values)/2]) / 2
	}
	return values[len(values)/2]
}

func (s *Service) Evaluate(ctx context.Context, runID string) ([]Evaluation, error) {
	run, err := s.repo.GetRun(runID)
	if err != nil {
		return nil, err
	}
	corpus, err := s.repo.GetCorpus(run.CorpusID)
	if err != nil {
		return nil, err
	}
	if !corpus.Sample || len(corpus.Questions) == 0 {
		return nil, errors.New("quality evaluation is available for the sample corpus")
	}
	out := []Evaluation{}
	type cachedQuery struct {
		vector []float32
		ms     int64
		err    error
	}
	queryCache := map[string]cachedQuery{}
	for _, pairRun := range run.Pairs {
		if pairRun.Status != "ready" {
			continue
		}
		e := Evaluation{Pair: pairRun.Pair, IndexMS: pairRun.IndexMS}
		var embeds, searches []int64
		for _, q := range corpus.Questions {
			key := pairRun.Pair.Model + "\x00" + q.Query
			cached, exists := queryCache[key]
			if !exists {
				embedder := s.embedders[pairRun.Pair.Model]
				if embedder == nil {
					cached.err = errors.New("embedding provider not configured")
				} else {
					start := time.Now()
					cached.vector, cached.err = embedder.Query(ctx, q.Query)
					cached.ms = time.Since(start).Milliseconds()
				}
				queryCache[key] = cached
			}
			result := PairResult{Pair: pairRun.Pair, EmbedMS: cached.ms}
			err := cached.err
			if err == nil {
				store := s.stores[pairRun.Pair.Store]
				if store == nil {
					err = errors.New("storage provider not configured")
				} else {
					start := time.Now()
					result.Hits, err = store.Search(ctx, resourceName(runID, pairRun.Pair.Model), cached.vector, 5)
					result.SearchMS = time.Since(start).Milliseconds()
				}
			}
			if err != nil {
				e.Error = err.Error()
				break
			}
			ids := make([]string, len(result.Hits))
			for i, h := range result.Hits {
				ids[i] = h.DocumentID
			}
			m := ScoreAtFive(ids, q.RelevantIDs)
			e.RecallAt5 += m.Recall
			e.MRRAt5 += m.MRR
			e.Queries++
			embeds = append(embeds, result.EmbedMS)
			searches = append(searches, result.SearchMS)
		}
		if e.Queries > 0 {
			e.RecallAt5 /= float64(e.Queries)
			e.MRRAt5 /= float64(e.Queries)
		}
		e.MedianEmbedMS = median(embeds)
		e.MedianSearchMS = median(searches)
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil, errors.New("no ready pairs")
	}
	s.mu.Lock()
	run, err = s.repo.GetRun(runID)
	if err == nil {
		run.LatestEvaluation = out
		err = s.repo.SaveRun(run)
	}
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return out, nil
}
