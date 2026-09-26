package providers

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
	"vectorlab/internal/lab"
)

var resourcePattern = regexp.MustCompile(`^vl_[a-f0-9]{24}_(openai|minilm|qwen)$`)

type Postgres struct{ URL string }

func (p Postgres) connect(ctx context.Context) (*pgx.Conn, error) {
	if p.URL == "" {
		return nil, errors.New("POSTGRES_URL is not set")
	}
	return pgx.Connect(ctx, p.URL)
}
func (p Postgres) Ready(ctx context.Context) error {
	c, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	return c.Ping(ctx)
}
func (p Postgres) Upsert(ctx context.Context, resource string, dim int, chunks []lab.Chunk) error {
	if !resourcePattern.MatchString(resource) {
		return errors.New("invalid resource name")
	}
	if dim < 1 || dim > 2000 {
		return errors.New("pgvector dimension must be 1 to 2000")
	}
	c, err := p.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	if _, err = c.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return err
	}
	table := pgx.Identifier{resource}.Sanitize()
	if _, err = c.Exec(ctx, fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (id TEXT PRIMARY KEY, document_id TEXT NOT NULL, title TEXT NOT NULL, content TEXT NOT NULL, embedding vector(%d) NOT NULL)", table, dim)); err != nil {
		return err
	}
	if _, err = c.Exec(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (embedding vector_cosine_ops)", pgx.Identifier{resource + "_cosine"}.Sanitize(), table)); err != nil {
		return err
	}
	tx, err := c.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	statement := fmt.Sprintf("INSERT INTO %s (id,document_id,title,content,embedding) VALUES($1,$2,$3,$4,$5::vector) ON CONFLICT(id) DO UPDATE SET document_id=excluded.document_id,title=excluded.title,content=excluded.content,embedding=excluded.embedding", table)
	for _, chunk := range chunks {
		v, err := vectorLiteral(chunk.Vector)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, statement, chunk.ID, chunk.DocumentID, chunk.Title, chunk.Text, v); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
func (p Postgres) Search(ctx context.Context, resource string, vector []float32, limit int) ([]lab.Hit, error) {
	if !resourcePattern.MatchString(resource) {
		return nil, errors.New("invalid resource name")
	}
	v, err := vectorLiteral(vector)
	if err != nil {
		return nil, err
	}
	c, err := p.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	query := fmt.Sprintf("SELECT document_id,title,content,1-(embedding <=> $1::vector) AS score FROM %s ORDER BY embedding <=> $1::vector LIMIT $2", pgx.Identifier{resource}.Sanitize())
	rows, err := c.Query(ctx, query, v, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	hits := []lab.Hit{}
	for rows.Next() {
		var h lab.Hit
		if err = rows.Scan(&h.DocumentID, &h.Title, &h.Text, &h.Score); err != nil {
			return nil, err
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}
