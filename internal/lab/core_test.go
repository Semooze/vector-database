package lab

import (
	"strings"
	"testing"
)

func TestChunkTextKeepsWordsAndOverlap(t *testing.T) {
	words := strings.Fields(strings.Repeat("trail ", 105))
	chunks := ChunkText(strings.Join(words, " "), 100, 20)
	if len(chunks) != 2 || len(strings.Fields(chunks[0])) != 100 || len(strings.Fields(chunks[1])) != 25 {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestMetricsUseRelevantDocumentIDs(t *testing.T) {
	got := ScoreAtFive([]string{"a", "b", "c"}, []string{"b", "d"})
	if got.Recall != .5 || got.MRR != .5 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestValidatePairsRejectsDuplicateAndInvalid(t *testing.T) {
	if err := ValidatePairs([]Pair{{Store: "postgres", Model: "qwen"}, {Store: "postgres", Model: "qwen"}}); err == nil {
		t.Fatal("duplicate pair accepted")
	}
	if err := ValidatePairs([]Pair{{Store: "unknown", Model: "qwen"}}); err == nil {
		t.Fatal("unknown store accepted")
	}
}
