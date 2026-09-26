package lab

import (
	_ "embed"
	"encoding/json"
)

//go:embed sample.json
var sampleJSON []byte

func SampleCorpus() (Corpus, error) {
	var c Corpus
	err := json.Unmarshal(sampleJSON, &c)
	return c, err
}
