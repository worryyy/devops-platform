package user

import (
	"context"
	stderrors "errors"
	"strings"

	apperrors "campus-forum/internal/pkg/errors"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*PublicUser, error) {
	username := strings.TrimSpace(req.Username)
	password := req.Password
	nickname := strings.TrimSpace(req.Nickname)

	if username == "" {
		return nil, apperrors.BadRequest("username is required")
	}
	if password == "" {
		return nil, apperrors.BadRequest("password is required")
	}
	if nickname == "" {
		nickname = username
	}

	existing, err := s.repo.FindByUsername(ctx, username)
	if err == nil && existing != nil {
		return nil, apperrors.Conflict("username already exists")
	}
	if err != nil && !stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	user := &User{
		Username:     username,
		PasswordHash: string(hash),
		Nickname:     nickname,
	}
	if err := s.repo.Create(ctx, user); err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return nil, apperrors.Conflict("username already exists")
		}
		return nil, err
	}

	return ToPublicUser(user), nil
}

func (s *Service) Authenticate(ctx context.Context, username string, password string) (*PublicUser, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, apperrors.BadRequest("username and password are required")
	}

	user, err := s.repo.FindByUsername(ctx, username)
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.Unauthorized("invalid username or password")
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, apperrors.Unauthorized("invalid username or password")
	}

	return ToPublicUser(user), nil
}

func (s *Service) GetPublicUser(ctx context.Context, id uint64) (*PublicUser, error) {
	if id == 0 {
		return nil, apperrors.BadRequest("user_id is required")
	}

	user, err := s.repo.FindByID(ctx, id)
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperrors.NotFound("user not found")
	}
	if err != nil {
		return nil, err
	}

	return ToPublicUser(user), nil
}

func (s *Service) GetUserSnapshot(ctx context.Context, id uint64) (*PublicUser, error) {
	return s.GetPublicUser(ctx, id)
}
