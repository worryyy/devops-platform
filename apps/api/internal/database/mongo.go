package database

import (
	"context"
	"time"

	"campus-forum/internal/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

func NewMongo(ctx context.Context, cfg config.Config) (*mongo.Client, *mongo.Database, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return nil, nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, nil, err
	}

	return client, client.Database(cfg.MongoDatabase), nil
}

func PingMongo(ctx context.Context, db *mongo.Database) error {
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	return db.Client().Ping(pingCtx, readpref.Primary())
}
