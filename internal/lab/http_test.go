package lab

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSampleEndpointIsIdempotent(t *testing.T) {
	repo, err := OpenRepository(filepath.Join(t.TempDir(), "lab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	handler := NewHTTPHandler(NewService(repo, nil, nil))
	for i := 0; i < 2; i++ {
		request := httptest.NewRequest(http.MethodPost, "/api/corpora/sample", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	}
	corpora, err := repo.ListCorpora()
	if err != nil || len(corpora) != 1 {
		t.Fatalf("corpora=%v err=%v", corpora, err)
	}
}
