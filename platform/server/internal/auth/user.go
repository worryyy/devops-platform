package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// Roles ordered from least to most privileged; kept as plain strings to stay
// interchangeable with the users.role column.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

var ValidRoles = []string{RoleViewer, RoleAdmin}

type User struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"uniqueIndex;size:64" json:"username"`
	PasswordHash string    `json:"-"`
	Role         string    `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (User) TableName() string { return "users" }

type UserStore struct{ db *gorm.DB }

func NewUserStore(db *gorm.DB) *UserStore { return &UserStore{db: db} }

var ErrInvalidCredentials = errors.New("invalid username or password")

func (s *UserStore) Authenticate(ctx context.Context, username, password string) (User, error) {
	var user User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Constant-ish work either way so a missing user costs the same
			// as a wrong password.
			_ = bcrypt.CompareHashAndPassword(
				[]byte("$2a$12$C6UzMDM.H6dfI/f/IKcEeO7ZBpEyR2eKok4Ma8V0PJXcgWKpZS2fi"), []byte(password))
			return User{}, ErrInvalidCredentials
		}
		return User{}, fmt.Errorf("query user: %w", err)
	}
	if !CheckPassword(user.PasswordHash, password) {
		return User{}, ErrInvalidCredentials
	}
	return user, nil
}

func (s *UserStore) Create(ctx context.Context, username, password, role string) (User, error) {
	if err := ValidateNewUser(username, password, role); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	user := User{Username: username, PasswordHash: hash, Role: role}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func ValidateNewUser(username, password, role string) error {
	if u := strings.TrimSpace(username); len(u) < 3 || len(u) > 64 {
		return fmt.Errorf("username must be 3-64 characters")
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	valid := false
	for _, r := range ValidRoles {
		if role == r {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("role must be one of %v", ValidRoles)
	}
	return nil
}
