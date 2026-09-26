package lab

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

type Question struct {
	Query       string   `json:"query"`
	RelevantIDs []string `json:"relevant_ids"`
}

type Corpus struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Sample    bool       `json:"sample"`
	Documents []Document `json:"documents"`
	Questions []Question `json:"questions,omitempty"`
}

type PairRun struct {
	Pair    Pair   `json:"pair"`
	Status  string `json:"status"`
	Error   string `json:"error,omitempty"`
	IndexMS int64  `json:"index_ms,omitempty"`
}

type Run struct {
	ID               string          `json:"id"`
	CorpusID         string          `json:"corpus_id"`
	CreatedAt        time.Time       `json:"created_at"`
	Pairs            []PairRun       `json:"pairs"`
	Latest           *SearchResponse `json:"latest,omitempty"`
	LatestEvaluation []Evaluation    `json:"latest_evaluation,omitempty"`
}

type Repository struct{ db *sql.DB }

func OpenRepository(path string) (*Repository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"CREATE TABLE IF NOT EXISTS corpora (id TEXT PRIMARY KEY, payload TEXT NOT NULL)",
		"CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, created_at TEXT NOT NULL, payload TEXT NOT NULL)",
	} {
		if _, err = db.Exec(statement); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &Repository{db: db}, nil
}

func (r *Repository) Close() error { return r.db.Close() }

func (r *Repository) SaveCorpus(c Corpus) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = r.db.Exec("INSERT INTO corpora(id,payload) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", c.ID, string(b))
	return err
}

func (r *Repository) GetCorpus(id string) (Corpus, error) {
	var raw string
	err := r.db.QueryRow("SELECT payload FROM corpora WHERE id=?", id).Scan(&raw)
	if err != nil {
		return Corpus{}, err
	}
	var c Corpus
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}

func (r *Repository) ListCorpora() ([]Corpus, error) {
	rows, err := r.db.Query("SELECT payload FROM corpora ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Corpus{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var c Corpus
		if err = json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *Repository) SaveRun(run Run) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = time.Now().UTC()
	}
	b, err := json.Marshal(run)
	if err != nil {
		return err
	}
	_, err = r.db.Exec("INSERT INTO runs(id,created_at,payload) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", run.ID, run.CreatedAt.Format(time.RFC3339Nano), string(b))
	return err
}

func (r *Repository) GetRun(id string) (Run, error) {
	var raw string
	err := r.db.QueryRow("SELECT payload FROM runs WHERE id=?", id).Scan(&raw)
	if err != nil {
		return Run{}, err
	}
	var run Run
	err = json.Unmarshal([]byte(raw), &run)
	return run, err
}

func (r *Repository) ListRuns() ([]Run, error) {
	rows, err := r.db.Query("SELECT payload FROM runs ORDER BY created_at DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Run{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var run Run
		if err = json.Unmarshal([]byte(raw), &run); err != nil {
			return nil, err
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (r *Repository) MarkInterrupted() error {
	runs, err := r.ListRuns()
	if err != nil {
		return err
	}
	for _, run := range runs {
		changed := false
		for i := range run.Pairs {
			if run.Pairs[i].Status == "queued" || run.Pairs[i].Status == "indexing" {
				run.Pairs[i].Status = "failed"
				run.Pairs[i].Error = "indexing interrupted by restart; create a new run"
				changed = true
			}
		}
		if changed {
			if err = r.SaveRun(run); err != nil {
				return err
			}
		}
	}
	return nil
}

var ErrNotFound = errors.New("not found")
