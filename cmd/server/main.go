package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"vectorlab/internal/lab"
	"vectorlab/internal/providers"
)

func setting(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func main() {
	dbPath := setting("LAB_DB_PATH", ".data/lab.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0700); err != nil {
		log.Fatal(err)
	}
	repo, err := lab.OpenRepository(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	if err := repo.MarkInterrupted(); err != nil {
		log.Fatal(err)
	}
	embedders := map[string]lab.Embedder{
		"openai": providers.OpenAI{Key: os.Getenv("OPENAI_API_KEY")},
		"minilm": providers.Worker{URL: setting("EMBED_WORKER_URL", "http://127.0.0.1:8090"), Model: "minilm", Dimensions: 384},
		"qwen":   providers.Worker{URL: setting("EMBED_WORKER_URL", "http://127.0.0.1:8090"), Model: "qwen", Dimensions: 1024},
	}
	stores := map[string]lab.VectorStore{
		"postgres": providers.Postgres{URL: os.Getenv("POSTGRES_URL")},
		"mongo":    providers.Mongo{URI: os.Getenv("MONGO_URI"), Database: setting("MONGO_DATABASE", "vectorlab")},
		"weaviate": providers.Weaviate{URL: os.Getenv("WEAVIATE_URL"), APIKey: os.Getenv("WEAVIATE_API_KEY")},
		"pinecone": providers.Pinecone{APIKey: os.Getenv("PINECONE_API_KEY"), Cloud: setting("PINECONE_CLOUD", "aws"), Region: setting("PINECONE_REGION", "us-east-1")},
	}
	service := lab.NewService(repo, embedders, stores)
	address := setting("LAB_API_ADDRESS", "127.0.0.1:8080")
	log.Printf("Vector Lab API listening on http://%s", address)
	log.Fatal(http.ListenAndServe(address, lab.NewHTTPHandler(service)))
}
