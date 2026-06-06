package comment

import "time"

type CreateRequest struct {
	TopicID string `json:"topic_id" binding:"required"`
	UserID  uint64 `json:"user_id" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type DeleteRequest struct {
	CommentID string `json:"comment_id" binding:"required"`
	UserID    uint64 `json:"user_id" binding:"required"`
}

type ListItem struct {
	ID         string    `json:"id"`
	TopicID    string    `json:"topic_id"`
	AuthorID   uint64    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type UserCommentItem struct {
	ID         string    `json:"id"`
	Content    string    `json:"content"`
	TopicID    string    `json:"topic_id"`
	TopicTitle string    `json:"topic_title"`
	CreatedAt  time.Time `json:"created_at"`
}
