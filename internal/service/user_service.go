// Package service holds the business logic of the application. Services
// orchestrate repositories and enforce domain rules, keeping handlers thin.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
)

// SyncInput carries the Keycloak identity claims needed to provision a local
// user. Kept here (rather than importing pkg/keycloak) so the service has no
// dependency on the OIDC layer.
type SyncInput struct {
	Sub        string
	Email      string
	Username   string
	GivenName  string
	FamilyName string
}

// UserService defines user read operations plus JIT provisioning from Keycloak.
type UserService interface {
	GetUser(ctx context.Context, id int64) (*repository.User, error)
	ListUsers(ctx context.Context) ([]*repository.User, error)
	// SyncFromKeycloak maps a Keycloak identity to a local user, creating it on
	// first sight and refreshing email/username when Keycloak's copy changed.
	SyncFromKeycloak(ctx context.Context, in SyncInput) (*repository.User, error)
	// AcceptTerms records user acceptance of the current T&C version.
	// Returns 400 TERMS_VERSION_MISMATCH if the provided version doesn't match the current version.
	AcceptTerms(ctx context.Context, userID int64, version string) error
}

// userService is the default UserService backed by a UserRepository. It also
// provisions the initial subscription (default plan) for newly created users.
type userService struct {
	repo                repository.UserRepository
	plans               repository.PlanRepository
	defaultPlanCode     string
	termsCurrentVersion string
	now                 func() time.Time
}

// NewUserService wires a UserService to its repositories. defaultPlanCode is the
// plan (e.g. "basic") assigned to a user on first provisioning. termsCurrentVersion
// is the active T&C version that users must accept.
func NewUserService(repo repository.UserRepository, plans repository.PlanRepository, defaultPlanCode, termsCurrentVersion string) UserService {
	return &userService{repo: repo, plans: plans, defaultPlanCode: defaultPlanCode, termsCurrentVersion: termsCurrentVersion, now: time.Now}
}

// SyncFromKeycloak returns the local user for a Keycloak sub, provisioning it on
// first sight (JIT). The unique index on keycloak_sub makes get-or-create
// race-safe: a concurrent create loses on the constraint and is re-fetched.
func (s *userService) SyncFromKeycloak(ctx context.Context, in SyncInput) (*repository.User, error) {
	// Required claims must be present and non-empty. Username and email back
	// NOT NULL unique columns; empty values from a misconfigured token/client
	// would collide on the unique index and map distinct users to one row.
	if in.Sub == "" || in.Email == "" || in.Username == "" {
		return nil, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED",
			"token is missing required claims (sub, email, preferred_username)")
	}

	user, err := s.repo.GetByKeycloakSub(ctx, in.Sub)
	if err == nil {
		if user.Email != in.Email || user.Username != in.Username {
			user.Email, user.Username = in.Email, in.Username
			name := in.GivenName + " " + in.FamilyName
			user.Name = &name
			updated, uerr := s.repo.Update(ctx, user)
			if uerr != nil {
				return nil, apperror.Internal(uerr)
			}
			return updated, nil
		}
		return user, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		return nil, apperror.Internal(fmt.Errorf("userService.SyncFromKeycloak: %w", err))
	}

	// New user: resolve the default plan and provision the user together with an
	// active subscription in one transaction.
	plan, err := s.plans.GetByCode(ctx, s.defaultPlanCode)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("userService.SyncFromKeycloak: %w", err)) // default plan is seeded; absence is a misconfiguration
	}

	now := s.now().UTC()
	sub := in.Sub
	name := in.GivenName + " " + in.FamilyName
	newUser := &repository.User{
		KeycloakSub: &sub,
		Email:       in.Email,
		Username:    in.Username,
		Name:        &name,
		CreatedAt:   now,
	}
	newSub := &repository.Subscription{
		PlanID:             plan.ID,
		Status:             "active",
		CurrentPeriodStart: now,
	}

	created, err := s.repo.CreateWithSubscription(ctx, newUser, newSub)
	if errors.Is(err, repository.ErrConflict) {
		// Lost a creation race — the winner's row (with its subscription) exists.
		winner, gerr := s.repo.GetByKeycloakSub(ctx, in.Sub)
		if gerr != nil {
			return nil, apperror.Internal(gerr)
		}
		return winner, nil
	}
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("userService.SyncFromKeycloak: %w", err))
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
		return nil, apperror.Internal(fmt.Errorf("userService.GetUser: %w", err))
	}
	return user, nil
}

// ListUsers returns all users.
func (s *userService) ListUsers(ctx context.Context) ([]*repository.User, error) {
	users, err := s.repo.List(ctx)
	if err != nil {
		return nil, apperror.Internal(fmt.Errorf("userService.ListUsers: %w", err))
	}
	return users, nil
}

// AcceptTerms records user acceptance of the current T&C version.
func (s *userService) AcceptTerms(ctx context.Context, userID int64, version string) error {
	// Validate that the client is accepting the current version
	if version != s.termsCurrentVersion {
		return apperror.New(http.StatusBadRequest, "TERMS_VERSION_MISMATCH",
			fmt.Sprintf("expected version %s, got %s", s.termsCurrentVersion, version))
	}

	acceptedAt := s.now().UTC()
	if err := s.repo.UpdateTermsAccepted(ctx, userID, version, acceptedAt); err != nil {
		return err
	}

	slog.Debug("terms accepted", "user_id", userID, "version", version, "accepted_at", acceptedAt)
	return nil
}
