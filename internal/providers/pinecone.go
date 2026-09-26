package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"vectorlab/internal/lab"
)

type Pinecone struct{ APIKey, Cloud, Region, ControlURL string }

func (p Pinecone) control() string {
	if p.ControlURL != "" {
		return strings.TrimRight(p.ControlURL, "/")
	}
	return "https://api.pinecone.io"
}
func (p Pinecone) call(ctx context.Context, method, url string, body, out any) (int, error) {
	if p.APIKey == "" {
		return 0, errors.New("PINECONE_API_KEY is not set")
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Api-Key", p.APIKey)
	req.Header.Set("X-Pinecone-API-Version", "2025-10")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("Pinecone %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		if err = json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}
func (p Pinecone) Ready(ctx context.Context) error {
	var response any
	_, err := p.call(ctx, "GET", p.control()+"/indexes", nil, &response)
	return err
}
func (p Pinecone) index(ctx context.Context, resource string, dim int, create bool) (string, error) {
	name := pineconeIndex(resource)
	var response struct {
		Host      string `json:"host"`
		Dimension int    `json:"dimension"`
		Status    struct {
			Ready bool `json:"ready"`
		} `json:"status"`
	}
	status, err := p.call(ctx, "GET", p.control()+"/indexes/"+name, nil, &response)
	if status == http.StatusNotFound && create {
		cloud := p.Cloud
		if cloud == "" {
			cloud = "aws"
		}
		region := p.Region
		if region == "" {
			region = "us-east-1"
		}
		_, err = p.call(ctx, "POST", p.control()+"/indexes", map[string]any{"name": name, "dimension": dim, "metric": "cosine", "spec": map[string]any{"serverless": map[string]any{"cloud": cloud, "region": region}}}, &response)
	}
	if err != nil {
		return "", err
	}
	if response.Dimension != dim {
		return "", fmt.Errorf("Pinecone index %s has dimension %d, expected %d", name, response.Dimension, dim)
	}
	if !response.Status.Ready {
		deadline := time.NewTimer(90 * time.Second)
		defer deadline.Stop()
		for !response.Status.Ready {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-deadline.C:
				return "", errors.New("Pinecone index not ready after 90 seconds")
			case <-time.After(2 * time.Second):
			}
			_, err = p.call(ctx, "GET", p.control()+"/indexes/"+name, nil, &response)
			if err != nil {
				return "", err
			}
		}
	}
	if response.Host == "" {
		return "", errors.New("Pinecone did not return an index host")
	}
	return "https://" + strings.TrimPrefix(strings.TrimPrefix(response.Host, "https://"), "http://"), nil
}
func (p Pinecone) Upsert(ctx context.Context, resource string, dim int, chunks []lab.Chunk) error {
	if !resourcePattern.MatchString(resource) {
		return errors.New("invalid resource name")
	}
	host, err := p.index(ctx, resource, dim, true)
	if err != nil {
		return err
	}
	for start := 0; start < len(chunks); start += 100 {
		end := start + 100
		if end > len(chunks) {
			end = len(chunks)
		}
		vectors := make([]any, 0, end-start)
		for _, c := range chunks[start:end] {
			if len(c.Vector) != dim {
				return errors.New("vector dimension mismatch")
			}
			vectors = append(vectors, map[string]any{"id": c.ID, "values": c.Vector, "metadata": map[string]any{"document_id": c.DocumentID, "title": c.Title, "content": c.Text}})
		}
		var result struct {
			UpsertedCount int `json:"upsertedCount"`
		}
		if _, err = p.call(ctx, "POST", host+"/vectors/upsert", map[string]any{"namespace": resource, "vectors": vectors}, &result); err != nil {
			return err
		}
		if result.UpsertedCount != len(vectors) {
			return fmt.Errorf("Pinecone upserted %d of %d vectors", result.UpsertedCount, len(vectors))
		}
	}
	return nil
}
func (p Pinecone) Search(ctx context.Context, resource string, vector []float32, limit int) ([]lab.Hit, error) {
	if !resourcePattern.MatchString(resource) {
		return nil, errors.New("invalid resource name")
	}
	host, err := p.index(ctx, resource, len(vector), false)
	if err != nil {
		return nil, err
	}
	var response struct {
		Matches []struct {
			Score    float64 `json:"score"`
			Metadata struct {
				DocumentID string `json:"document_id"`
				Title      string `json:"title"`
				Content    string `json:"content"`
			} `json:"metadata"`
		} `json:"matches"`
	}
	_, err = p.call(ctx, "POST", host+"/query", map[string]any{"namespace": resource, "vector": vector, "topK": limit, "includeMetadata": true}, &response)
	if err != nil {
		return nil, err
	}
	hits := []lab.Hit{}
	for _, match := range response.Matches {
		hits = append(hits, lab.Hit{DocumentID: match.Metadata.DocumentID, Title: match.Metadata.Title, Text: match.Metadata.Content, Score: match.Score})
	}
	return hits, nil
}
