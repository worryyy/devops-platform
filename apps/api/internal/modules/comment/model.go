package comment

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const CollectionName = "comments"

type Comment struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TopicID    primitive.ObjectID `bson:"topic_id" json:"topic_id"`
	AuthorID   uint64             `bson:"author_id" json:"author_id"`
	AuthorName string             `bson:"author_name" json:"author_name"`
	Content    string             `bson:"content" json:"content"`
	CreatedAt  time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time          `bson:"updated_at" json:"updated_at"`
	DeletedAt  *time.Time         `bson:"deleted_at,omitempty" json:"deleted_at,omitempty"`
}
