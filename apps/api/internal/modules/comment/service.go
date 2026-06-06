package comment

import (
	"context"
	"strings"
	"time"

	"campus-forum/internal/modules/topic"
	"campus-forum/internal/modules/user"
	apperrors "campus-forum/internal/pkg/errors"
	"campus-forum/internal/pkg/pagination"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type UserProvider interface {
	GetUserSnapshot(ctx context.Context, id uint64) (*user.PublicUser, error)
}

type TopicProvider interface {
	FindActiveTopic(ctx context.Context, id primitive.ObjectID) (*topic.Topic, error)
	IncrementCommentCount(ctx context.Context, id primitive.ObjectID, delta int64) error
}

type Service struct {
	repo   *Repository
	users  UserProvider
	topics TopicProvider
}

func NewService(repo *Repository, users UserProvider, topics TopicProvider) *Service {
	return &Service{
		repo:   repo,
		users:  users,
		topics: topics,
	}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*ListItem, error) {
	if req.UserID == 0 {
		return nil, apperrors.BadRequest("user_id is required")
	}

	topicID, err := parseObjectID(req.TopicID, "topic_id")
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(req.Content)
	if content == "" {
		return nil, apperrors.BadRequest("content is required")
	}

	author, err := s.users.GetUserSnapshot(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if _, err := s.topics.FindActiveTopic(ctx, topicID); err != nil {
		return nil, err
	}

	now := time.Now()
	comment := &Comment{
		TopicID:    topicID,
		AuthorID:   author.ID,
		AuthorName: author.Nickname,
		Content:    content,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Create(ctx, comment); err != nil {
		return nil, err
	}
	if err := s.topics.IncrementCommentCount(ctx, topicID, 1); err != nil {
		return nil, err
	}

	item := toListItem(*comment)
	return &item, nil
}

func (s *Service) ListByTopic(ctx context.Context, topicIDRaw string, p pagination.Params) (interface{}, int64, error) {
	topicID, err := parseObjectID(topicIDRaw, "topic_id")
	if err != nil {
		return nil, 0, err
	}

	if _, err := s.topics.FindActiveTopic(ctx, topicID); err != nil {
		return nil, 0, err
	}

	comments, total, err := s.repo.ListByTopic(ctx, topicID, p)
	if err != nil {
		return nil, 0, err
	}
	return toListItems(comments), total, nil
}

func (s *Service) ListByAuthor(ctx context.Context, userID uint64, p pagination.Params) (interface{}, int64, error) {
	if _, err := s.users.GetUserSnapshot(ctx, userID); err != nil {
		return nil, 0, err
	}

	items, total, err := s.repo.ListByAuthorWithActiveTopics(ctx, userID, p)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *Service) Delete(ctx context.Context, req DeleteRequest) error {
	if req.UserID == 0 {
		return apperrors.BadRequest("user_id is required")
	}

	commentID, err := parseObjectID(req.CommentID, "comment_id")
	if err != nil {
		return err
	}

	comment, err := s.repo.FindActiveByID(ctx, commentID)
	if err == mongo.ErrNoDocuments {
		return apperrors.NotFound("comment not found")
	}
	if err != nil {
		return err
	}
	if comment.AuthorID != req.UserID {
		return apperrors.Forbidden("only the comment author can delete this comment")
	}

	if err := s.repo.SoftDelete(ctx, commentID, time.Now()); err != nil {
		return err
	}
	return s.topics.IncrementCommentCount(ctx, comment.TopicID, -1)
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

func toListItems(comments []Comment) []ListItem {
	items := make([]ListItem, 0, len(comments))
	for _, comment := range comments {
		items = append(items, toListItem(comment))
	}
	return items
}

func toListItem(comment Comment) ListItem {
	return ListItem{
		ID:         comment.ID.Hex(),
		TopicID:    comment.TopicID.Hex(),
		AuthorID:   comment.AuthorID,
		AuthorName: comment.AuthorName,
		Content:    comment.Content,
		CreatedAt:  comment.CreatedAt,
	}
}
