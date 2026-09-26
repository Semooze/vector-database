package providers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"vectorlab/internal/lab"
)

type Weaviate struct{ URL, APIKey string }

func (w Weaviate) base() string { return strings.TrimRight(w.URL, "/") }
func (w Weaviate) Ready(ctx context.Context) error {
	if w.URL == "" {
		return errors.New("WEAVIATE_URL is not set")
	}
	_, err := requestJSON(ctx, "GET", w.base()+"/v1/meta", w.APIKey, nil, nil)
	return err
}
func weaviateClass(resource string) string { return "VL" + strings.ReplaceAll(resource, "_", "") }
func deterministicUUID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x-%x-%x-%x-%x", sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}
func (w Weaviate) Upsert(ctx context.Context, resource string, dim int, chunks []lab.Chunk) error {
	if !resourcePattern.MatchString(resource) {
		return errors.New("invalid resource name")
	}
	if err := w.Ready(ctx); err != nil {
		return err
	}
	class := weaviateClass(resource)
	status, err := requestJSON(ctx, "GET", w.base()+"/v1/schema/"+class, w.APIKey, nil, nil)
	if err != nil && status != http.StatusNotFound {
		return err
	}
	if status == http.StatusNotFound {
		body := map[string]any{"class": class, "vectorizer": "none", "vectorIndexConfig": map[string]any{"distance": "cosine"}, "properties": []any{map[string]any{"name": "document_id", "dataType": []string{"text"}}, map[string]any{"name": "title", "dataType": []string{"text"}}, map[string]any{"name": "content", "dataType": []string{"text"}}}}
		if _, err = requestJSON(ctx, "POST", w.base()+"/v1/schema", w.APIKey, body, nil); err != nil {
			return err
		}
	}
	for start := 0; start < len(chunks); start += 100 {
		end := start + 100
		if end > len(chunks) {
			end = len(chunks)
		}
		objects := make([]any, 0, end-start)
		for _, c := range chunks[start:end] {
			if len(c.Vector) != dim {
				return errors.New("vector dimension mismatch")
			}
			objects = append(objects, map[string]any{"class": class, "id": deterministicUUID(resource + ":" + c.ID), "properties": map[string]any{"document_id": c.DocumentID, "title": c.Title, "content": c.Text}, "vector": c.Vector})
		}
		var response []struct {
			Result struct {
				Errors map[string]any `json:"errors"`
			} `json:"result"`
		}
		if _, err = requestJSON(ctx, "POST", w.base()+"/v1/batch/objects", w.APIKey, map[string]any{"objects": objects}, &response); err != nil {
			return err
		}
		if len(response) != len(objects) {
			return errors.New("Weaviate returned incomplete batch")
		}
		for _, item := range response {
			if len(item.Result.Errors) > 0 {
				return fmt.Errorf("Weaviate batch error: %v", item.Result.Errors)
			}
		}
	}
	return nil
}
func (w Weaviate) Search(ctx context.Context, resource string, vector []float32, limit int) ([]lab.Hit, error) {
	if !resourcePattern.MatchString(resource) {
		return nil, errors.New("invalid resource name")
	}
	if err := w.Ready(ctx); err != nil {
		return nil, err
	}
	v, err := json.Marshal(vector)
	if err != nil {
		return nil, err
	}
	class := weaviateClass(resource)
	query := fmt.Sprintf("{Get{%s(nearVector:{vector:%s},limit:%d){document_id title content _additional{distance}}}}", class, v, limit)
	var response struct {
		Data struct {
			Get map[string][]struct {
				DocumentID string `json:"document_id"`
				Title      string `json:"title"`
				Content    string `json:"content"`
				Additional struct {
					Distance float64 `json:"distance"`
				} `json:"_additional"`
			} `json:"Get"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	_, err = requestJSON(ctx, "POST", w.base()+"/v1/graphql", w.APIKey, map[string]any{"query": query}, &response)
	if err != nil {
		return nil, err
	}
	if len(response.Errors) > 0 {
		return nil, fmt.Errorf("Weaviate query: %v", response.Errors)
	}
	hits := []lab.Hit{}
	for _, item := range response.Data.Get[class] {
		hits = append(hits, lab.Hit{DocumentID: item.DocumentID, Title: item.Title, Text: item.Content, Score: 1 - item.Additional.Distance})
	}
	return hits, nil
}
