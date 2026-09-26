package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
)

type OpenAI struct {
	Key, URL   string
	Dimensions int
}

func (e OpenAI) Ready(context.Context) error {
	if e.Key == "" {
		return errors.New("OPENAI_API_KEY is not set")
	}
	return nil
}
func (e OpenAI) Dimension() int {
	if e.Dimensions > 0 {
		return e.Dimensions
	}
	return 1536
}
func (e OpenAI) Documents(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, texts)
}
func (e OpenAI) Query(ctx context.Context, text string) ([]float32, error) {
	v, err := e.embed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}
func (e OpenAI) embed(ctx context.Context, texts []string) ([][]float32, error) {
	if e.Key == "" {
		return nil, errors.New("OPENAI_API_KEY is not set")
	}
	url := e.URL
	if url == "" {
		url = "https://api.openai.com/v1/embeddings"
	}
	var response struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	_, err := requestJSON(ctx, "POST", url, e.Key, map[string]any{"model": "text-embedding-3-small", "input": texts}, &response)
	if err != nil {
		return nil, err
	}
	if len(response.Data) != len(texts) {
		return nil, fmt.Errorf("OpenAI returned %d embeddings for %d texts", len(response.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for _, item := range response.Data {
		if item.Index < 0 || item.Index >= len(out) {
			return nil, errors.New("OpenAI returned invalid embedding index")
		}
		out[item.Index] = item.Embedding
	}
	for _, v := range out {
		if len(v) != e.Dimension() {
			return nil, errors.New("OpenAI returned unexpected dimension")
		}
	}
	return out, nil
}

type Worker struct {
	URL, Model string
	Dimensions int
}

func (e Worker) Ready(ctx context.Context) error {
	url := e.URL
	if url == "" {
		url = os.Getenv("EMBED_WORKER_URL")
	}
	if url == "" {
		url = "http://127.0.0.1:8090"
	}
	_, err := requestJSON(ctx, "GET", url+"/health", "", nil, nil)
	return err
}
func (e Worker) Dimension() int { return e.Dimensions }
func (e Worker) Documents(ctx context.Context, texts []string) ([][]float32, error) {
	return e.embed(ctx, "document", texts)
}
func (e Worker) Query(ctx context.Context, text string) ([]float32, error) {
	v, err := e.embed(ctx, "query", []string{text})
	if err != nil {
		return nil, err
	}
	return v[0], nil
}
func (e Worker) embed(ctx context.Context, kind string, texts []string) ([][]float32, error) {
	url := e.URL
	if url == "" {
		url = os.Getenv("EMBED_WORKER_URL")
	}
	if url == "" {
		url = "http://127.0.0.1:8090"
	}
	var response struct {
		Vectors [][]float32 `json:"vectors"`
	}
	_, err := requestJSON(ctx, "POST", url+"/embed", "", map[string]any{"model": e.Model, "kind": kind, "texts": texts}, &response)
	if err != nil {
		return nil, err
	}
	if len(response.Vectors) != len(texts) {
		return nil, errors.New("worker returned wrong embedding count")
	}
	for _, v := range response.Vectors {
		if len(v) != e.Dimension() {
			return nil, errors.New("worker returned wrong embedding dimension")
		}
	}
	return response.Vectors, nil
}
