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
// table schema; the matching SQL lives in migrations/000001, 000004 and 000009.
// Identity is owned by Keycloak: KeycloakSub links a local row to a Keycloak
// user (UUID `sub`), and users are JIT-provisioned on first authenticated call.
type User struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	KeycloakSub *string   `gorm:"size:36;uniqueIndex" json:"-"` // Keycloak `sub`; nil for legacy rows
	Username    string    `gorm:"size:255;uniqueIndex;not null" json:"username"`
	Email       string    `gorm:"size:255;uniqueIndex;not null" json:"email"`
	Name        *string   `gorm:"size:255" json:"name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserRepository defines the persistence operations for users.
type UserRepository interface {
	Create(ctx context.Context, user *User) (*User, error)
	// CreateWithSubscription inserts a user and its initial subscription in one
	// transaction (both-or-nothing), so no user ever exists without a
	// subscription. sub.UserID is set from the created user.
	CreateWithSubscription(ctx context.Context, user *User, sub *Subscription) (*User, error)
	GetByID(ctx context.Context, id int64) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByKeycloakSub(ctx context.Context, sub string) (*User, error)
	Update(ctx context.Context, user *User) (*User, error)
	// UpdatePaddleCustomerID sets the paddle_customer_id on a user row.
	UpdatePaddleCustomerID(ctx context.Context, userID int64, customerID string) error
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

// CreateWithSubscription inserts the user and its initial subscription in a
// single transaction. A uniqueness violation on either table maps to
// ErrConflict so the caller can resolve a provisioning race.
func (r *userRepository) CreateWithSubscription(ctx context.Context, user *User, sub *Subscription) (*User, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		sub.UserID = user.ID
		return tx.Create(sub).Error
	})
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrConflict
	}
	if err != nil {
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

// GetByKeycloakSub returns the user linked to the given Keycloak sub or ErrNotFound.
func (r *userRepository) GetByKeycloakSub(ctx context.Context, sub string) (*User, error) {
	var user User
	if err := r.db.WithContext(ctx).Where("keycloak_sub = ?", sub).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

// Update persists changes to an existing user (e.g. email/username synced from
// Keycloak) and returns the stored row.
func (r *userRepository) Update(ctx context.Context, user *User) (*User, error) {
	if err := r.db.WithContext(ctx).Save(user).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return user, nil
}

// UpdatePaddleCustomerID sets the paddle_customer_id column for the given user.
func (r *userRepository) UpdatePaddleCustomerID(ctx context.Context, userID int64, customerID string) error {
	return r.db.WithContext(ctx).
		Model(&User{}).
		Where("id = ?", userID).
		Update("paddle_customer_id", customerID).Error
}

// List returns all users ordered by ID.
func (r *userRepository) List(ctx context.Context) ([]*User, error) {
	var users []*User
	if err := r.db.WithContext(ctx).Order("id").Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}
