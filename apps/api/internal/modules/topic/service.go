package topic

import (
	"context"
	"strings"
	"time"

	"campus-forum/internal/modules/user"
	apperrors "campus-forum/internal/pkg/errors"
	"campus-forum/internal/pkg/pagination"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type UserProvider interface {
	GetUserSnapshot(ctx context.Context, id uint64) (*user.PublicUser, error)
}

type Service struct {
	repo  *Repository
	users UserProvider
}

func NewService(repo *Repository, users UserProvider) *Service {
	return &Service{repo: repo, users: users}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*DetailResponse, error) {
	title := strings.TrimSpace(req.Title)
	content := strings.TrimSpace(req.Content)
	if req.UserID == 0 {
		return nil, apperrors.BadRequest("user_id is required")
	}
	if title == "" {
		return nil, apperrors.BadRequest("title is required")
	}
	if content == "" {
		return nil, apperrors.BadRequest("content is required")
	}

	author, err := s.users.GetUserSnapshot(ctx, req.UserID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	topic := &Topic{
		AuthorID:     author.ID,
		AuthorName:   author.Nickname,
		Title:        title,
		Content:      content,
		CommentCount: 0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := s.repo.Create(ctx, topic); err != nil {
		return nil, err
	}

	detail := toDetailResponse(*topic)
	return &detail, nil
}

func (s *Service) List(ctx context.Context, p pagination.Params) (interface{}, int64, error) {
	topics, total, err := s.repo.List(ctx, p)
	if err != nil {
		return nil, 0, err
	}
	return toListItems(topics), total, nil
}

func (s *Service) ListByAuthor(ctx context.Context, userID uint64, p pagination.Params) (interface{}, int64, error) {
	if _, err := s.users.GetUserSnapshot(ctx, userID); err != nil {
		return nil, 0, err
	}

	topics, total, err := s.repo.ListByAuthor(ctx, userID, p)
	if err != nil {
		return nil, 0, err
	}
	return toListItems(topics), total, nil
}

func (s *Service) Detail(ctx context.Context, topicID string) (*DetailResponse, error) {
	id, err := parseObjectID(topicID, "topic_id")
	if err != nil {
		return nil, err
	}

	topic, err := s.FindActiveTopic(ctx, id)
	if err != nil {
		return nil, err
	}

	detail := toDetailResponse(*topic)
	return &detail, nil
}

func (s *Service) Delete(ctx context.Context, req DeleteRequest) error {
	if req.UserID == 0 {
		return apperrors.BadRequest("user_id is required")
	}

	id, err := parseObjectID(req.TopicID, "topic_id")
	if err != nil {
		return err
	}

	topic, err := s.FindActiveTopic(ctx, id)
	if err != nil {
		return err
	}
	if topic.AuthorID != req.UserID {
		return apperrors.Forbidden("only the topic author can delete this topic")
	}

	return s.repo.SoftDelete(ctx, id, time.Now())
}

func (s *Service) Search(ctx context.Context, keyword string, p pagination.Params) (interface{}, int64, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return []ListItem{}, 0, nil
	}

	topics, total, err := s.repo.Search(ctx, keyword, p)
	if err != nil {
		return nil, 0, err
	}
	return toListItems(topics), total, nil
}

func (s *Service) FindActiveTopic(ctx context.Context, id primitive.ObjectID) (*Topic, error) {
	topic, err := s.repo.FindActiveByID(ctx, id)
	if err == mongo.ErrNoDocuments {
		return nil, apperrors.NotFound("topic not found")
	}
	if err != nil {
		return nil, err
	}
	return topic, nil
}

func (s *Service) IncrementCommentCount(ctx context.Context, id primitive.ObjectID, delta int64) error {
	return s.repo.IncrementCommentCount(ctx, id, delta, time.Now())
}

func parseObjectID(raw string, field string) (primitive.ObjectID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return primitive.NilObjectID, apperrors.BadRequest(field + " is required")
	}
	id, err := primitive.ObjectIDFromHex(raw)
	if err != nil {
		return primitive.NilObjectID, apperrors.BadRequest("invalid " + field)
	}
	return id, nil
}

func toListItems(topics []Topic) []ListItem {
	items := make([]ListItem, 0, len(topics))
	for _, topic := range topics {
		items = append(items, toListItem(topic))
	}
	return items
}

func toListItem(topic Topic) ListItem {
	return ListItem{
		ID:           topic.ID.Hex(),
		Title:        topic.Title,
		Summary:      summarize(topic.Content, 120),
		AuthorID:     topic.AuthorID,
		AuthorName:   topic.AuthorName,
		CommentCount: topic.CommentCount,
		CreatedAt:    topic.CreatedAt,
	}
}

func toDetailResponse(topic Topic) DetailResponse {
	return DetailResponse{
		ID:           topic.ID.Hex(),
		Title:        topic.Title,
		Content:      topic.Content,
		AuthorID:     topic.AuthorID,
		AuthorName:   topic.AuthorName,
		CommentCount: topic.CommentCount,
		CreatedAt:    topic.CreatedAt,
		UpdatedAt:    topic.UpdatedAt,
	}
}

func summarize(content string, maxRunes int) string {
	runes := []rune(content)
	if len(runes) <= maxRunes {
		return content
	}
	return string(runes[:maxRunes]) + "..."
}
