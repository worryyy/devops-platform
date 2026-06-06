package comment

import (
	"context"
	"time"

	"campus-forum/internal/modules/topic"
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
			Keys: bson.D{{Key: "topic_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "author_id", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "created_at", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "deleted_at", Value: 1}},
		},
	})
	return err
}

func (r *Repository) Create(ctx context.Context, comment *Comment) error {
	if comment.ID.IsZero() {
		comment.ID = primitive.NewObjectID()
	}
	_, err := r.collection.InsertOne(ctx, comment)
	return err
}

func (r *Repository) FindActiveByID(ctx context.Context, id primitive.ObjectID) (*Comment, error) {
	var comment Comment
	if err := r.collection.FindOne(ctx, withActiveCommentFilter(bson.M{"_id": id})).Decode(&comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

func (r *Repository) ListByTopic(ctx context.Context, topicID primitive.ObjectID, p pagination.Params) ([]Comment, int64, error) {
	return r.listByFilter(ctx, withActiveCommentFilter(bson.M{"topic_id": topicID}), p, bson.D{{Key: "created_at", Value: 1}})
}

func (r *Repository) ListByAuthorWithActiveTopics(ctx context.Context, authorID uint64, p pagination.Params) ([]UserCommentItem, int64, error) {
	base := mongo.Pipeline{
		bson.D{{Key: "$match", Value: withActiveCommentFilter(bson.M{"author_id": authorID})}},
		bson.D{{Key: "$lookup", Value: bson.D{
			{Key: "from", Value: topic.CollectionName},
			{Key: "localField", Value: "topic_id"},
			{Key: "foreignField", Value: "_id"},
			{Key: "as", Value: "topic"},
		}}},
		bson.D{{Key: "$unwind", Value: "$topic"}},
		bson.D{{Key: "$match", Value: bson.M{"topic.deleted_at": bson.M{"$exists": false}}}},
	}

	total, err := r.countAggregate(ctx, base)
	if err != nil {
		return nil, 0, err
	}

	listPipeline := append(mongo.Pipeline{}, base...)
	listPipeline = append(listPipeline,
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		bson.D{{Key: "$skip", Value: p.Offset()}},
		bson.D{{Key: "$limit", Value: p.Limit()}},
		bson.D{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 1},
			{Key: "content", Value: 1},
			{Key: "topic_id", Value: 1},
			{Key: "created_at", Value: 1},
			{Key: "topic_title", Value: "$topic.title"},
		}}},
	)

	cursor, err := r.collection.Aggregate(ctx, listPipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	rows := make([]struct {
		ID         primitive.ObjectID `bson:"_id"`
		Content    string             `bson:"content"`
		TopicID    primitive.ObjectID `bson:"topic_id"`
		TopicTitle string             `bson:"topic_title"`
		CreatedAt  time.Time          `bson:"created_at"`
	}, 0)
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, 0, err
	}

	items := make([]UserCommentItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, UserCommentItem{
			ID:         row.ID.Hex(),
			Content:    row.Content,
			TopicID:    row.TopicID.Hex(),
			TopicTitle: row.TopicTitle,
			CreatedAt:  row.CreatedAt,
		})
	}

	return items, total, nil
}

func (r *Repository) SoftDelete(ctx context.Context, id primitive.ObjectID, deletedAt time.Time) error {
	_, err := r.collection.UpdateOne(ctx, withActiveCommentFilter(bson.M{"_id": id}), bson.M{
		"$set": bson.M{
			"deleted_at": deletedAt,
			"updated_at": deletedAt,
		},
	})
	return err
}

func (r *Repository) listByFilter(ctx context.Context, filter bson.M, p pagination.Params, sort bson.D) ([]Comment, int64, error) {
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

	comments := make([]Comment, 0)
	for cursor.Next(ctx) {
		var comment Comment
		if err := cursor.Decode(&comment); err != nil {
			return nil, 0, err
		}
		comments = append(comments, comment)
	}
	if err := cursor.Err(); err != nil {
		return nil, 0, err
	}

	return comments, total, nil
}

func (r *Repository) countAggregate(ctx context.Context, base mongo.Pipeline) (int64, error) {
	countPipeline := append(mongo.Pipeline{}, base...)
	countPipeline = append(countPipeline, bson.D{{Key: "$count", Value: "total"}})

	cursor, err := r.collection.Aggregate(ctx, countPipeline)
	if err != nil {
		return 0, err
	}
	defer cursor.Close(ctx)

	var rows []struct {
		Total int64 `bson:"total"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Total, nil
}

func activeCommentFilter() bson.M {
	return bson.M{"deleted_at": bson.M{"$exists": false}}
}

func withActiveCommentFilter(extra bson.M) bson.M {
	filter := activeCommentFilter()
	for key, value := range extra {
		filter[key] = value
	}
	return filter
}
