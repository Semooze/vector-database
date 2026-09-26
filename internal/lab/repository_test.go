package lab

import (
	"path/filepath"
	"testing"
)

func TestRepositoryPersistsCorpusAndRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lab.db")
	repo, err := OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	corpus := Corpus{ID: "sample", Name: "Trails", Documents: []Document{{ID: "a", Title: "A", Text: "A shaded walk"}}}
	if err := repo.SaveCorpus(corpus); err != nil {
		t.Fatal(err)
	}
	run := Run{ID: "r1", CorpusID: "sample", Pairs: []PairRun{{Pair: Pair{Store: "postgres", Model: "minilm"}, Status: "queued"}}}
	if err := repo.SaveRun(run); err != nil {
		t.Fatal(err)
	}
	repo.Close()
	reopened, err := OpenRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetRun("r1")
	if err != nil || got.CorpusID != "sample" || len(got.Pairs) != 1 {
		t.Fatalf("run=%+v err=%v", got, err)
	}
	loaded, err := reopened.GetCorpus("sample")
	if err != nil || loaded.Documents[0].Text != "A shaded walk" {
		t.Fatalf("corpus=%+v err=%v", loaded, err)
	}
}
