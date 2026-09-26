package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var httpClient = &http.Client{Timeout: 5 * time.Minute}

func vectorLiteral(vector []float32) (string, error) {
	if len(vector) == 0 {
		return "", errors.New("empty vector")
	}
	parts := make([]string, len(vector))
	for i, v := range vector {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return "", errors.New("non-finite vector")
		}
		parts[i] = strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func pineconeIndex(resource string) string {
	parts := strings.Split(resource, "_")
	return "vectorlab-" + parts[len(parts)-1]
}

func requestJSON(ctx context.Context, method, url, apiKey string, body any, out any) (int, error) {
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
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	limited := io.LimitReader(resp.Body, 2<<20)
	b, err := io.ReadAll(limited)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}
