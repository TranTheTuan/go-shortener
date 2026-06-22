// Package repository contains the data-access layer. Repositories abstract
// the storage backend behind interfaces so the rest of the application does
// not depend on a concrete database. This implementation uses GORM.
package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrNotFound is returned when a requested entity does not exist. The service
// layer maps it onto an application error.
var ErrNotFound = errors.New("repository: not found")

// ErrConflict is returned when a uniqueness constraint is violated.
var ErrConflict = errors.New("repository: conflict")

// User is the domain entity persisted by UserRepository. GORM tags define the
// table schema; the matching SQL lives in migrations/000001 and 000004.
type User struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Username     string    `gorm:"size:255;uniqueIndex;not null" json:"username"`
	Email        string    `gorm:"size:255;uniqueIndex;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"` // never serialized to clients
	Name         *string   `gorm:"size:255" json:"name,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserRepository defines the persistence operations for users.
type UserRepository interface {
	Create(ctx context.Context, user *User) (*User, error)
	GetByID(ctx context.Context, id int64) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	List(ctx context.Context) ([]*User, error)
}

// userRepository is the GORM-backed UserRepository.
type userRepository struct {
	db *gorm.DB
}

// NewUserRepository wires a UserRepository to a GORM database handle.
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

// Create inserts a new user and returns the stored row.
func (r *userRepository) Create(ctx context.Context, user *User) (*User, error) {
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return user, nil
}

// GetByID returns the user with the given ID or ErrNotFound.
func (r *userRepository) GetByID(ctx context.Context, id int64) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).First(&user, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetByEmail returns the user with the given email or ErrNotFound.
func (r *userRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// GetByUsername returns the user with the given username or ErrNotFound.
func (r *userRepository) GetByUsername(ctx context.Context, username string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// List returns all users ordered by ID.
func (r *userRepository) List(ctx context.Context) ([]*User, error) {
	var users []*User
	if err := r.db.WithContext(ctx).Order("id").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}
