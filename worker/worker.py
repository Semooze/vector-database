"""Local embedding HTTP worker for the vector search lab."""

import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


MODEL_IDS = {
    "minilm": "sentence-transformers/all-MiniLM-L6-v2",
    "qwen": "Qwen/Qwen3-Embedding-0.6B",
}
DIMENSIONS = {"minilm": 384, "qwen": 1024}
_models = {}
_lock = threading.Lock()


def get_model(name):
    if name not in MODEL_IDS:
        raise ValueError(f"unknown model: {name}")
    with _lock:
        if name not in _models:
            from sentence_transformers import SentenceTransformer

            _models[name] = SentenceTransformer(MODEL_IDS[name])
        return _models[name]


def embed_texts(model, name, kind, texts):
    options = {"normalize_embeddings": True}
    if name == "qwen" and kind == "query":
        options["prompt_name"] = "query"
    vectors = model.encode(texts, **options)
    return vectors.tolist() if hasattr(vectors, "tolist") else vectors


class Handler(BaseHTTPRequestHandler):
    def respond(self, status, payload):
        encoded = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def do_GET(self):
        if self.path == "/health":
            self.respond(200, {"status": "ok", "loaded_models": list(_models)})
            return
        self.respond(404, {"error": "not found"})

    def do_POST(self):
        if self.path != "/embed":
            self.respond(404, {"error": "not found"})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if length <= 0 or length > 8 * 1024 * 1024:
                raise ValueError("request size must be 1 to 8 MB")
            body = json.loads(self.rfile.read(length))
            name, kind, texts = body["model"], body["kind"], body["texts"]
            if name not in MODEL_IDS or kind not in ("query", "document"):
                raise ValueError("invalid model or kind")
            if not isinstance(texts, list) or not texts or any(not isinstance(t, str) or not t.strip() for t in texts):
                raise ValueError("texts must be a nonempty list of strings")
            model = get_model(name)
            with _lock:
                vectors = embed_texts(model, name, kind, texts)
            if any(len(v) != DIMENSIONS[name] for v in vectors):
                raise ValueError("model returned unexpected dimension")
            self.respond(200, {"vectors": vectors})
        except (ValueError, KeyError, TypeError, json.JSONDecodeError) as exc:
            self.respond(400, {"error": str(exc)})
        except Exception as exc:
            self.respond(503, {"error": f"embedding failed: {exc}"})


def main():
    host = os.environ.get("EMBED_WORKER_HOST", "127.0.0.1")
    port = int(os.environ.get("EMBED_WORKER_PORT", "8090"))
    server = ThreadingHTTPServer((host, port), Handler)
    print(f"Embedding worker listening on http://{host}:{port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
