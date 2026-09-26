package lab

import (
	"errors"
	"fmt"
	"strings"
)

type Pair struct {
	Store string `json:"store"`
	Model string `json:"model"`
}

type Document struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type Chunk struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	Title      string    `json:"title"`
	Text       string    `json:"text"`
	Vector     []float32 `json:"-"`
}

type Hit struct {
	DocumentID string  `json:"document_id"`
	Title      string  `json:"title"`
	Text       string  `json:"text"`
	Score      float64 `json:"score"`
}

type Metrics struct {
	Recall float64 `json:"recall_at_5"`
	MRR    float64 `json:"mrr_at_5"`
}

type PairResult struct {
	Pair     Pair   `json:"pair"`
	Hits     []Hit  `json:"hits"`
	EmbedMS  int64  `json:"embed_ms"`
	SearchMS int64  `json:"search_ms"`
	Error    string `json:"error,omitempty"`
}

type SearchResponse struct {
	Query   string       `json:"query"`
	Results []PairResult `json:"results"`
}

func ChunkText(text string, size, overlap int) []string {
	words := strings.Fields(text)
	if len(words) == 0 || size <= 0 || overlap < 0 || overlap >= size {
		return nil
	}
	var chunks []string
	for start := 0; start < len(words); start += size - overlap {
		end := start + size
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[start:end], " "))
		if end == len(words) {
			break
		}
	}
	return chunks
}

func ScoreAtFive(results, relevant []string) Metrics {
	if len(relevant) == 0 {
		return Metrics{}
	}
	set := make(map[string]bool, len(relevant))
	for _, id := range relevant {
		set[id] = true
	}
	seen := map[string]bool{}
	var hits int
	var mrr float64
	for i, id := range results {
		if i == 5 {
			break
		}
		if set[id] && !seen[id] {
			seen[id] = true
			hits++
			if mrr == 0 {
				mrr = 1 / float64(i+1)
			}
		}
	}
	return Metrics{Recall: float64(hits) / float64(len(set)), MRR: mrr}
}

func ValidatePairs(pairs []Pair) error {
	if len(pairs) < 2 || len(pairs) > 4 {
		return errors.New("select 2 to 4 pairs")
	}
	stores := map[string]bool{"postgres": true, "mongo": true, "weaviate": true, "pinecone": true}
	models := map[string]bool{"openai": true, "minilm": true, "qwen": true}
	seen := map[Pair]bool{}
	for _, p := range pairs {
		if !stores[p.Store] || !models[p.Model] {
			return fmt.Errorf("unknown pair: %s/%s", p.Store, p.Model)
		}
		if seen[p] {
			return fmt.Errorf("duplicate pair: %s/%s", p.Store, p.Model)
		}
		seen[p] = true
	}
	return nil
}
