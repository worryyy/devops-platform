package topic

import (
	"context"
	"regexp"

	"campus-forum/internal/pkg/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Repository struct {
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) *Repository {
	return &Repository{collection: db.Collection(CollectionName)}
}

func (r *Repository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "author_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "deleted_at", Value: 1}},
		},
		{
			Keys: bson.D{
				{Key: "title", Value: "text"},
				{Key: "content", Value: "text"},
				{Key: "author_name", Value: "text"},
			},
			Options: options.Index().SetName("idx_topics_text"),
		},
	})
	return err
}

func (r *Repository) Create(ctx context.Context, topic *Topic) error {
	if topic.ID.IsZero() {
		topic.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, topic)
	return err
}

func (r *Repository) FindActiveByID(ctx context.Context, id primitive.ObjectID) (*Topic, error) {
	var topic Topic
	if err := r.collection.FindOne(ctx, withActiveFilter(bson.M{"_id": id})).Decode(&topic); err != nil {
		return nil, err
	}
	return &topic, nil
}

func (r *Repository) List(ctx context.Context, p pagination.Params) ([]Topic, int64, error) {
	return r.listByFilter(ctx, activeFilter(), p, bson.D{{Key: "created_at", Value: -1}})
}

func (r *Repository) ListByAuthor(ctx context.Context, authorID uint64, p pagination.Params) ([]Topic, int64, error) {
	return r.listByFilter(ctx, withActiveFilter(bson.M{"author_id": authorID}), p, bson.D{{Key: "created_at", Value: -1}})
}

func (r *Repository) Search(ctx context.Context, keyword string, p pagination.Params) ([]Topic, int64, error) {
	regex := primitive.Regex{Pattern: regexp.QuoteMeta(keyword), Options: "i"}
	filter := withActiveFilter(bson.M{
		"$or": bson.A{
			bson.M{"title": regex},
			bson.M{"content": regex},
			bson.M{"author_name": regex},
		},
	})
	return r.listByFilter(ctx, filter, p, bson.D{{Key: "created_at", Value: -1}})
}

func (r *Repository) SoftDelete(ctx context.Context, id primitive.ObjectID, deletedAt interface{}) error {
	_, err := r.collection.UpdateOne(ctx, withActiveFilter(bson.M{"_id": id}), bson.M{
		"$set": bson.M{
			"deleted_at": deletedAt,
			"updated_at": deletedAt,
		},
	})
	return err
}

func (r *Repository) IncrementCommentCount(ctx context.Context, id primitive.ObjectID, delta int64, updatedAt interface{}) error {
	_, err := r.collection.UpdateOne(ctx, withActiveFilter(bson.M{"_id": id}), bson.M{
		"$inc": bson.M{"comment_count": delta},
		"$set": bson.M{"updated_at": updatedAt},
	})
	return err
}

func (r *Repository) listByFilter(ctx context.Context, filter bson.M, p pagination.Params, sort bson.D) ([]Topic, int64, error) {
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	cursor, err := r.collection.Find(ctx, filter, options.Find().
		SetSort(sort).
		SetSkip(p.Offset()).
		SetLimit(p.Limit()))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	topics := make([]Topic, 0)
	for cursor.Next(ctx) {
		var topic Topic
		if err := cursor.Decode(&topic); err != nil {
			return nil, 0, err
		}
		topics = append(topics, topic)
	}
	if err := cursor.Err(); err != nil {
		return nil, 0, err
	}

	return topics, total, nil
}

func activeFilter() bson.M {
	return bson.M{"deleted_at": bson.M{"$exists": false}}
}

func withActiveFilter(extra bson.M) bson.M {
	filter := activeFilter()
	for key, value := range extra {
		filter[key] = value
	}
	return filter
}
