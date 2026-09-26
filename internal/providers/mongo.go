package providers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"vectorlab/internal/lab"
)

type Mongo struct{ URI, Database string }

func (m Mongo) connect(ctx context.Context) (*mongo.Client, error) {
	if m.URI == "" {
		return nil, errors.New("MONGO_URI is not set")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(m.URI))
	if err != nil {
		return nil, err
	}
	if err = client.Ping(ctx, readpref.Primary()); err != nil {
		client.Disconnect(ctx)
		return nil, err
	}
	return client, nil
}
func (m Mongo) Ready(ctx context.Context) error {
	c, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Disconnect(ctx)
	return nil
}
func (m Mongo) collection(c *mongo.Client, resource string) *mongo.Collection {
	db := m.Database
	if db == "" {
		db = "vectorlab"
	}
	return c.Database(db).Collection(resource)
}
func (m Mongo) Upsert(ctx context.Context, resource string, dim int, chunks []lab.Chunk) error {
	if !resourcePattern.MatchString(resource) {
		return errors.New("invalid resource name")
	}
	c, err := m.connect(ctx)
	if err != nil {
		return err
	}
	defer c.Disconnect(ctx)
	col := m.collection(c, resource)
	for _, chunk := range chunks {
		v := make([]float64, len(chunk.Vector))
		for i, x := range chunk.Vector {
			v[i] = float64(x)
		}
		doc := bson.M{"_id": chunk.ID, "document_id": chunk.DocumentID, "title": chunk.Title, "content": chunk.Text, "embedding": v}
		if _, err = col.ReplaceOne(ctx, bson.M{"_id": chunk.ID}, doc, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
	}
	indexes := col.SearchIndexes()
	cursor, err := indexes.List(ctx, options.SearchIndexes().SetName("vector_index"))
	if err != nil {
		return err
	}
	exists := cursor.Next(ctx)
	cursor.Close(ctx)
	if err = cursor.Err(); err != nil {
		return err
	}
	if !exists {
		definition := bson.M{"fields": bson.A{bson.M{"type": "vector", "path": "embedding", "numDimensions": dim, "similarity": "cosine"}}}
		_, err = indexes.CreateOne(ctx, mongo.SearchIndexModel{Definition: definition, Options: options.SearchIndexes().SetName("vector_index").SetType("vectorSearch")})
		if err != nil {
			return err
		}
	}
	deadline := time.NewTimer(90 * time.Second)
	defer deadline.Stop()
	for {
		cursor, err = indexes.List(ctx, options.SearchIndexes().SetName("vector_index"))
		if err != nil {
			return err
		}
		ready := false
		for cursor.Next(ctx) {
			var item struct {
				Queryable bool `bson:"queryable"`
			}
			if cursor.Decode(&item) == nil && item.Queryable {
				ready = true
			}
		}
		cursor.Close(ctx)
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("MongoDB vector index not ready after 90 seconds")
		case <-time.After(2 * time.Second):
		}
	}
}
func (m Mongo) Search(ctx context.Context, resource string, vector []float32, limit int) ([]lab.Hit, error) {
	if !resourcePattern.MatchString(resource) {
		return nil, errors.New("invalid resource name")
	}
	c, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer c.Disconnect(ctx)
	v := make([]float64, len(vector))
	for i, x := range vector {
		v[i] = float64(x)
	}
	pipeline := mongo.Pipeline{
		{{Key: "$vectorSearch", Value: bson.D{{Key: "index", Value: "vector_index"}, {Key: "path", Value: "embedding"}, {Key: "queryVector", Value: v}, {Key: "numCandidates", Value: 100}, {Key: "limit", Value: limit}}}},
		{{Key: "$project", Value: bson.D{{Key: "document_id", Value: 1}, {Key: "title", Value: 1}, {Key: "content", Value: 1}, {Key: "score", Value: bson.D{{Key: "$meta", Value: "vectorSearchScore"}}}}}},
	}
	cursor, err := m.collection(c, resource).Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	hits := []lab.Hit{}
	for cursor.Next(ctx) {
		var item struct {
			DocumentID string  `bson:"document_id"`
			Title      string  `bson:"title"`
			Content    string  `bson:"content"`
			Score      float64 `bson:"score"`
		}
		if err = cursor.Decode(&item); err != nil {
			return nil, err
		}
		hits = append(hits, lab.Hit{DocumentID: item.DocumentID, Title: item.Title, Text: item.Content, Score: item.Score})
	}
	return hits, cursor.Err()
}
