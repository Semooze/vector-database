package lab

import "testing"

func TestSampleCorpusHasJudgedQuestions(t *testing.T) {
	c, err := SampleCorpus()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Sample || len(c.Documents) != 30 || len(c.Questions) != 10 {
		t.Fatalf("sample size: docs=%d questions=%d", len(c.Documents), len(c.Questions))
	}
	ids := map[string]bool{}
	for _, d := range c.Documents {
		ids[d.ID] = true
	}
	for _, q := range c.Questions {
		if len(q.RelevantIDs) == 0 {
			t.Fatal("unjudged question")
		}
		for _, id := range q.RelevantIDs {
			if !ids[id] {
				t.Fatalf("unknown relevance id %s", id)
			}
		}
	}
}
