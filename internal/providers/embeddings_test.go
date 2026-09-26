package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOpenAIEmbedsDocumentsInInputOrder(t *testing.T) {
	previous := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Error("missing authorization")
		}
		var request struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&request)
		if len(request.Input) != 2 {
			t.Errorf("input=%v", request.Input)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"index":1,"embedding":[0,1]},{"index":0,"embedding":[1,0]}]}`)), Header: http.Header{}}, nil
	})}
	defer func() { httpClient = previous }()
	client := OpenAI{Key: "key", URL: "http://example.test", Dimensions: 2}
	vectors, err := client.Documents(context.Background(), []string{"first", "second"})
	if err != nil || vectors[0][0] != 1 || vectors[1][1] != 1 {
		t.Fatalf("vectors=%v err=%v", vectors, err)
	}
}

func TestOpenAIReadinessRequiresKey(t *testing.T) {
	if err := (OpenAI{}).Ready(context.Background()); err == nil {
		t.Fatal("OpenAI reported ready without a key")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
