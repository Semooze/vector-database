package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"vectorlab/internal/lab"
)

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}
}

func TestPineconeUpsertAndSearch(t *testing.T) {
	previous := httpClient
	resource := "vl_0123456789abcdef01234567_minilm"
	var upserted bool
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Api-Key") != "key" {
			t.Error("missing Pinecone key")
		}
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/indexes/vectorlab-minilm"):
			return jsonResponse(200, `{"dimension":2,"host":"pc.test","status":{"ready":true}}`), nil
		case r.Method == "POST" && r.URL.Path == "/vectors/upsert":
			upserted = true
			return jsonResponse(200, `{"upsertedCount":1}`), nil
		case r.Method == "POST" && r.URL.Path == "/query":
			return jsonResponse(200, `{"matches":[{"score":0.9,"metadata":{"document_id":"d","title":"Trail","content":"shaded"}}]}`), nil
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		return jsonResponse(404, `{}`), nil
	})}
	defer func() { httpClient = previous }()
	store := Pinecone{APIKey: "key", ControlURL: "https://control.test"}
	if err := store.Upsert(context.Background(), resource, 2, []lab.Chunk{{ID: "c", DocumentID: "d", Title: "Trail", Text: "shaded", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(context.Background(), resource, []float32{1, 0}, 5)
	if err != nil || !upserted || len(hits) != 1 || hits[0].DocumentID != "d" {
		t.Fatalf("hits=%v upserted=%v err=%v", hits, upserted, err)
	}
}

func TestPineconeRejectsIncompleteUpsert(t *testing.T) {
	previous := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "GET" {
			return jsonResponse(200, `{"dimension":2,"host":"pc.test","status":{"ready":true}}`), nil
		}
		return jsonResponse(200, `{"upsertedCount":0}`), nil
	})}
	defer func() { httpClient = previous }()
	store := Pinecone{APIKey: "key", ControlURL: "https://control.test"}
	err := store.Upsert(context.Background(), "vl_0123456789abcdef01234567_minilm", 2, []lab.Chunk{{ID: "c", Vector: []float32{1, 0}}})
	if err == nil {
		t.Fatal("incomplete upsert accepted")
	}
}

func TestWeaviateUpsertAndSearch(t *testing.T) {
	previous := httpClient
	resource := "vl_0123456789abcdef01234567_minilm"
	class := weaviateClass(resource)
	var created, inserted bool
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1/meta":
			return jsonResponse(200, `{}`), nil
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/schema/"):
			return jsonResponse(404, `{"error":"missing"}`), nil
		case r.Method == "POST" && r.URL.Path == "/v1/schema":
			created = true
			return jsonResponse(200, `{}`), nil
		case r.Method == "POST" && r.URL.Path == "/v1/batch/objects":
			inserted = true
			return jsonResponse(200, `[{"result":{"errors":null}}]`), nil
		case r.Method == "POST" && r.URL.Path == "/v1/graphql":
			return jsonResponse(200, `{"data":{"Get":{"`+class+`":[{"document_id":"d","title":"Trail","content":"shaded","_additional":{"distance":0.1}}]}}}`), nil
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		return jsonResponse(404, `{}`), nil
	})}
	defer func() { httpClient = previous }()
	store := Weaviate{URL: "http://weaviate.test"}
	if err := store.Upsert(context.Background(), resource, 2, []lab.Chunk{{ID: "c", DocumentID: "d", Title: "Trail", Text: "shaded", Vector: []float32{1, 0}}}); err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(context.Background(), resource, []float32{1, 0}, 5)
	if err != nil || !created || !inserted || len(hits) != 1 || hits[0].DocumentID != "d" {
		t.Fatalf("hits=%v created=%v inserted=%v err=%v", hits, created, inserted, err)
	}
}
