// Package service holds the business logic of the application. Services
// orchestrate repositories and enforce domain rules, keeping handlers thin.
package service

import (
	"context"
	"errors"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// UserService defines the read operations available for users. User creation is
// owned by AuthService.Register (see internal/service/auth_service.go).
type UserService interface {
	GetUser(ctx context.Context, id int64) (*repository.User, error)
	ListUsers(ctx context.Context) ([]*repository.User, error)
}

// userService is the default UserService backed by a UserRepository.
type userService struct {
	repo repository.UserRepository
}

// NewUserService wires a UserService to its repository.
func NewUserService(repo repository.UserRepository) UserService {
	return &userService{repo: repo}
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
