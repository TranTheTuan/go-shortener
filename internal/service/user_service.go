// Package service holds the business logic of the application. Services
// orchestrate repositories and enforce domain rules, keeping handlers thin.
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/TranTheTuan/YOUR-REPO-NAME/internal/repository"
	"github.com/TranTheTuan/YOUR-REPO-NAME/pkg/apperror"
)

// CreateUserInput carries the data required to create a user.
type CreateUserInput struct {
	Name  string
	Email string
}

// UserService defines the business operations available for users.
type UserService interface {
	CreateUser(ctx context.Context, in CreateUserInput) (*repository.User, error)
	GetUser(ctx context.Context, id int64) (*repository.User, error)
	ListUsers(ctx context.Context) ([]*repository.User, error)
}

// userService is the default UserService backed by a UserRepository.
type userService struct {
	repo repository.UserRepository
	now  func() time.Time
}

// NewUserService wires a UserService to its repository.
func NewUserService(repo repository.UserRepository) UserService {
	return &userService{
		repo: repo,
		now:  time.Now,
	}
}

// CreateUser validates the input and persists a new user.
func (s *userService) CreateUser(ctx context.Context, in CreateUserInput) (*repository.User, error) {
	name := strings.TrimSpace(in.Name)
	email := strings.TrimSpace(in.Email)

	if name == "" {
		return nil, apperror.BadRequest("name is required")
	}
	if !strings.Contains(email, "@") {
		return nil, apperror.BadRequest("a valid email is required")
	}

	user := &repository.User{
		Name:      name,
		Email:     email,
		CreatedAt: s.now().UTC(),
	}

	created, err := s.repo.Create(ctx, user)
	if errors.Is(err, repository.ErrConflict) {
		return nil, apperror.Conflict("a user with this email already exists")
	}
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return created, nil
}

// GetUser returns a single user by ID.
func (s *userService) GetUser(ctx context.Context, id int64) (*repository.User, error) {
	user, err := s.repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.NotFound("user not found")
	}
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return user, nil
}

// ListUsers returns all users.
func (s *userService) ListUsers(ctx context.Context) ([]*repository.User, error) {
	users, err := s.repo.List(ctx)
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return users, nil
}
