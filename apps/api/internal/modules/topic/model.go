package topic

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const CollectionName = "topics"

type Topic struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	AuthorID     uint64             `bson:"author_id" json:"author_id"`
	AuthorName   string             `bson:"author_name" json:"author_name"`
	Title        string             `bson:"title" json:"title"`
	Content      string             `bson:"content" json:"content"`
	CommentCount int64              `bson:"comment_count" json:"comment_count"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
	DeletedAt    *time.Time         `bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
}
