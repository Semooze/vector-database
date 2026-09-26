# Vector Field Notes

A local comparison lab for semantic search. Index the same corpus with 2–4 combinations of PostgreSQL/pgvector, a local MongoDB Atlas deployment, Weaviate, or Pinecone and OpenAI, Sentence-Transformers, or local Qwen embeddings. The browser shows ranked passages side by side and evaluates a fictional hiking corpus with Recall@5, MRR@5, index time, query embedding time, and search time.

## Requirements

- Go 1.26, Node 24, Docker, and [uv](https://docs.astral.sh/uv/).
- [Atlas CLI](https://www.mongodb.com/docs/atlas/cli/current/atlas-cli-deploy-local/) for local MongoDB Vector Search; its Docker daemon must be running. Use MongoDB 7.0.5 or later.
- An OpenAI API key for OpenAI embeddings and a Pinecone API key for Pinecone. Both are optional when testing only local providers. The first local Qwen use downloads about 1.2 GB of model weights.

## Start the lab

1. Start Docker Desktop. Then run `docker compose up -d` in this directory. This starts PostgreSQL on port 5433 and Weaviate on port 8081.
2. Create your local Atlas deployment with `atlas local setup vectorlab`. Get its connection string with `atlas local connect vectorlab --connectWith connectionString`. The MongoDB adapter creates its own collections and vector indexes; no Atlas sample data is required.
3. Copy `.env.example` to `.env`, set `MONGO_URI` to the connection string from step 2, and add any API keys you want to use. Source the file in your shell with `set -a; source .env; set +a`.
4. In one terminal, run `uv run --project worker --python 3.11 python -m worker.worker`. The worker listens on port 8090 and loads each local model on first use.
5. In a second terminal with the environment sourced, run `go run ./cmd/server`.
6. In a third terminal, run `cd web && npm install --include=dev && npm run dev`. Open the displayed browser URL.

Click **Load sample hiking corpus**, select two or more store/model pairs, and click **Index selected pairs**. Indexing runs in the background and reports each pair’s status. Then search or run the sample evaluation. You can upload up to 20 `.txt` or `.md` files, with a combined request limit of 5 MB. Uploaded corpora show search results and timing; judged relevance scores are available for the sample corpus.

The app stores corpus text, run status, and the latest search/evaluation in `.data/lab.sqlite`. Each run creates isolated storage resources. Pinecone uses one serverless index per model and a namespace per run; creating these resources may incur charges. The app binds locally and has no authentication, so keep it on a trusted development machine.

## Verify

```sh
GOCACHE=/private/tmp/vectorlab-go-cache go test ./...
uv run --project worker --python 3.11 python -m unittest worker.test_worker
cd web && npm test && npm run build
```

The Go and frontend checks run without provider credentials. A live integration check requires the selected services, model downloads, and keys to be available.

## API

The Go service exposes `GET /api/providers`, `GET /api/corpora`, `POST /api/corpora/sample`, multipart `POST /api/corpora`, `GET /api/runs`, `POST /api/runs`, `GET /api/runs/{id}`, `POST /api/runs/{id}/search`, and `POST /api/runs/{id}/evaluate`. `POST /api/runs` accepts `{ "corpus_id": "...", "pairs": [{ "store": "postgres", "model": "minilm" }] }` with 2–4 distinct pairs. Search accepts `{ "query": "..." }` and returns the top five passages from each ready pair.
