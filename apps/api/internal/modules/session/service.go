package session

import (
	"context"

	"campus-forum/internal/modules/user"
)

type UserAuthenticator interface {
	Authenticate(ctx context.Context, username string, password string) (*user.PublicUser, error)
}

type Service struct {
	users UserAuthenticator
}

func NewService(users UserAuthenticator) *Service {
	return &Service{users: users}
}

func (s *Service) Login(ctx context.Context, req LoginRequest) (*user.PublicUser, error) {
	return s.users.Authenticate(ctx, req.Username, req.Password)
}
