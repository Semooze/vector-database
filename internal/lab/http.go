package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type httpHandler struct{ service *Service }

func NewHTTPHandler(service *Service) http.Handler {
	h := &httpHandler{service: service}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/providers", h.providers)
	mux.HandleFunc("GET /api/corpora", h.listCorpora)
	mux.HandleFunc("POST /api/corpora/sample", h.sample)
	mux.HandleFunc("POST /api/corpora", h.upload)
	mux.HandleFunc("GET /api/runs", h.listRuns)
	mux.HandleFunc("POST /api/runs", h.createRun)
	mux.HandleFunc("GET /api/runs/{id}", h.getRun)
	mux.HandleFunc("POST /api/runs/{id}/search", h.search)
	mux.HandleFunc("POST /api/runs/{id}/evaluate", h.evaluate)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func badRequest(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
}
func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func (h *httpHandler) providers(w http.ResponseWriter, r *http.Request) {
	type state struct {
		Ready bool   `json:"ready"`
		Error string `json:"error,omitempty"`
	}
	stores := map[string]state{}
	var mu sync.Mutex
	var checks sync.WaitGroup
	for _, name := range []string{"postgres", "mongo", "weaviate", "pinecone"} {
		checks.Add(1)
		go func(name string) {
			defer checks.Done()
			store := h.service.stores[name]
			value := state{}
			if store == nil {
				value.Error = "not configured"
			} else {
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				err := store.Ready(ctx)
				cancel()
				if err != nil {
					value.Error = err.Error()
				} else {
					value.Ready = true
				}
			}
			mu.Lock()
			stores[name] = value
			mu.Unlock()
		}(name)
	}
	models := map[string]state{}
	for _, name := range []string{"openai", "minilm", "qwen"} {
		checks.Add(1)
		go func(name string) {
			defer checks.Done()
			embedder := h.service.embedders[name]
			value := state{}
			if embedder == nil {
				value.Error = "not configured"
			} else if check, ok := embedder.(interface{ Ready(context.Context) error }); ok {
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				err := check.Ready(ctx)
				cancel()
				if err != nil {
					value.Error = err.Error()
				} else {
					value.Ready = true
				}
			} else {
				value.Ready = true
			}
			mu.Lock()
			models[name] = value
			mu.Unlock()
		}(name)
	}
	checks.Wait()
	writeJSON(w, 200, map[string]any{"stores": stores, "models": models})
}
func (h *httpHandler) listCorpora(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.repo.ListCorpora()
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 200, items)
}
func (h *httpHandler) sample(w http.ResponseWriter, r *http.Request) {
	c, err := SampleCorpus()
	if err != nil {
		badRequest(w, err)
		return
	}
	if err = h.service.repo.SaveCorpus(c); err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
func (h *httpHandler) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
	if err := r.ParseMultipartForm(5 << 20); err != nil {
		badRequest(w, fmt.Errorf("upload exceeds 5 MB or is invalid: %w", err))
		return
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 || len(files) > 20 {
		badRequest(w, fmt.Errorf("upload 1 to 20 files"))
		return
	}
	documents := make([]Document, 0, len(files))
	for _, header := range files {
		ext := strings.ToLower(filepath.Ext(header.Filename))
		if ext != ".txt" && ext != ".md" {
			badRequest(w, fmt.Errorf("only .txt and .md files are accepted"))
			return
		}
		file, err := header.Open()
		if err != nil {
			badRequest(w, err)
			return
		}
		content, err := io.ReadAll(io.LimitReader(file, 5<<20))
		file.Close()
		if err != nil {
			badRequest(w, err)
			return
		}
		if !utf8.Valid(content) || strings.TrimSpace(string(content)) == "" {
			badRequest(w, fmt.Errorf("%s must contain UTF-8 text", header.Filename))
			return
		}
		documents = append(documents, Document{ID: newID(), Title: filepath.Base(header.Filename), Text: string(content)})
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		name = "Uploaded corpus"
	}
	if len(name) > 100 {
		badRequest(w, fmt.Errorf("corpus name is too long"))
		return
	}
	c := Corpus{ID: newID(), Name: name, Documents: documents}
	if err := h.service.repo.SaveCorpus(c); err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}
func (h *httpHandler) listRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.service.repo.ListRuns()
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 200, runs)
}
func (h *httpHandler) createRun(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CorpusID string `json:"corpus_id"`
		Pairs    []Pair `json:"pairs"`
	}
	if err := decodeJSON(r, &input); err != nil {
		badRequest(w, err)
		return
	}
	run, err := h.service.CreateRun(input.CorpusID, input.Pairs)
	if err != nil {
		badRequest(w, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		h.service.IndexRun(ctx, run.ID)
	}()
	writeJSON(w, http.StatusAccepted, run)
}
func (h *httpHandler) getRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.service.repo.GetRun(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "run not found"})
		return
	}
	writeJSON(w, 200, run)
}
func (h *httpHandler) search(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Query string `json:"query"`
	}
	if err := decodeJSON(r, &input); err != nil {
		badRequest(w, err)
		return
	}
	result, err := h.service.Search(r.Context(), r.PathValue("id"), input.Query)
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (h *httpHandler) evaluate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.Evaluate(r.Context(), r.PathValue("id"))
	if err != nil {
		badRequest(w, err)
		return
	}
	writeJSON(w, 200, result)
}
