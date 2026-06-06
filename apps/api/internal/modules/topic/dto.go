package topic

import "time"

type CreateRequest struct {
	UserID  uint64 `json:"user_id" binding:"required"`
	Title   string `json:"title" binding:"required"`
	Content string `json:"content" binding:"required"`
}

type DeleteRequest struct {
	TopicID string `json:"topic_id" binding:"required"`
	UserID  uint64 `json:"user_id" binding:"required"`
}

type ListItem struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary"`
	AuthorID     uint64    `json:"author_id"`
	AuthorName   string    `json:"author_name"`
	CommentCount int64     `json:"comment_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type DetailResponse struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	AuthorID     uint64    `json:"author_id"`
	AuthorName   string    `json:"author_name"`
	CommentCount int64     `json:"comment_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
