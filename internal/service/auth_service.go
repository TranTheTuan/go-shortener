package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// usernamePattern allows 3–50 chars of letters, digits, and underscores.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,50}$`)

// minPasswordLen is the minimum accepted password length.
const minPasswordLen = 8

// errInvalidCredentials is the single, generic auth failure (no user enumeration).
var errInvalidCredentials = apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "invalid email or password")

// RegisterInput carries the data required to register a user.
type RegisterInput struct {
	Username string
	Email    string
	Password string
	Name     *string
}

// LoginInput carries login credentials. Authentication is by email only.
type LoginInput struct {
	Email    string
	Password string
}

// TokenPair is the credential set returned on login/refresh.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"` // access-token lifetime in seconds
}

// AuthService defines the authentication operations.
type AuthService interface {
	Register(ctx context.Context, in RegisterInput) (*repository.User, error)
	Login(ctx context.Context, in LoginInput) (*TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (*TokenPair, error)
	Logout(ctx context.Context, refreshToken string) error
}

// authService is the default AuthService.
type authService struct {
	users      repository.UserRepository
	refresh    repository.RefreshTokenRepository
	issuer     *token.Issuer
	refreshTTL time.Duration
	bcryptCost int
	now        func() time.Time
}

// NewAuthService wires an AuthService to its dependencies.
func NewAuthService(
	users repository.UserRepository,
	refresh repository.RefreshTokenRepository,
	issuer *token.Issuer,
	refreshTTL time.Duration,
	bcryptCost int,
) AuthService {
	return &authService{
		users:      users,
		refresh:    refresh,
		issuer:     issuer,
		refreshTTL: refreshTTL,
		bcryptCost: bcryptCost,
		now:        time.Now,
	}
}

// Register validates the input, hashes the password, and persists a new user.
func (s *authService) Register(ctx context.Context, in RegisterInput) (*repository.User, error) {
	username := strings.TrimSpace(in.Username)
	email := strings.TrimSpace(in.Email)

	if !usernamePattern.MatchString(username) {
		return nil, apperror.BadRequest("username must be 3-50 chars: letters, digits, underscore")
	}
	if !strings.Contains(email, "@") {
		return nil, apperror.BadRequest("a valid email is required")
	}
	if len(in.Password) < minPasswordLen {
		return nil, apperror.BadRequest("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), s.bcryptCost)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	user := &repository.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		Name:         in.Name,
		CreatedAt:    s.now().UTC(),
	}

	created, err := s.users.Create(ctx, user)
	if errors.Is(err, repository.ErrConflict) {
		return nil, apperror.Conflict("username or email already taken")
	}
	if err != nil {
		return nil, apperror.Internal(err)
	}
	return created, nil
}

// Login authenticates by email + password and issues a token pair.
func (s *authService) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	user, err := s.users.GetByEmail(ctx, strings.TrimSpace(in.Email))
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errInvalidCredentials
	}
	if err != nil {
		return nil, apperror.Internal(err)
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)) != nil {
		return nil, errInvalidCredentials
	}

	return s.issueTokenPair(ctx, user.ID)
}

// Refresh validates a refresh token, rotates it, and issues a new pair.
func (s *authService) Refresh(ctx context.Context, refreshToken string) (*TokenPair, error) {
	hash := hashToken(refreshToken)
	rt, err := s.refresh.GetByHash(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errInvalidCredentials
	}
	if err != nil {
		return nil, apperror.Internal(err)
	}
	if rt.RevokedAt != nil || s.now().UTC().After(rt.ExpiresAt) {
		return nil, errInvalidCredentials
	}

	// Rotation: invalidate the presented token before issuing a new pair.
	if err := s.refresh.Revoke(ctx, rt.ID); err != nil {
		return nil, apperror.Internal(err)
	}
	return s.issueTokenPair(ctx, rt.UserID)
}

// Logout revokes the given refresh token. It is idempotent: an unknown or
// already-revoked token is not an error (and reveals nothing).
func (s *authService) Logout(ctx context.Context, refreshToken string) error {
	rt, err := s.refresh.GetByHash(ctx, hashToken(refreshToken))
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return apperror.Internal(err)
	}
	if rt.RevokedAt != nil {
		return nil
	}
	if err := s.refresh.Revoke(ctx, rt.ID); err != nil {
		return apperror.Internal(err)
	}
	return nil
}

// issueTokenPair mints an access token and a fresh, stored refresh token.
func (s *authService) issueTokenPair(ctx context.Context, userID int64) (*TokenPair, error) {
	access, err := s.issuer.Issue(userID)
	if err != nil {
		return nil, apperror.Internal(err)
	}

	raw, err := newRefreshToken()
	if err != nil {
		return nil, apperror.Internal(err)
	}

	_, err = s.refresh.Create(ctx, &repository.RefreshToken{
		UserID:    userID,
		TokenHash: hashToken(raw),
		ExpiresAt: s.now().UTC().Add(s.refreshTTL),
		CreatedAt: s.now().UTC(),
	})
	if err != nil {
		return nil, apperror.Internal(err)
	}

	return &TokenPair{
		AccessToken:  access,
		RefreshToken: raw,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.issuer.AccessTTL().Seconds()),
	}, nil
}

// newRefreshToken returns a 32-byte crypto-random token, base64url-encoded.
func newRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded sha256 of a raw refresh token. Only this
// digest is ever stored, so a database leak cannot reveal usable tokens.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
