package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/token"
)

// fixedNow is a deterministic clock for token expiry math.
var fixedNow = time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)

// newAuthSvc builds an authService backed by in-memory mocks and a fixed clock.
func newAuthSvc() (*authService, *mockUserRepo, *mockRefreshRepo) {
	users := newMockUserRepo()
	refresh := newMockRefreshRepo()
	svc := &authService{
		users:      users,
		refresh:    refresh,
		issuer:     token.NewIssuer("test-secret", 15*time.Minute),
		refreshTTL: 168 * time.Hour,
		bcryptCost: bcrypt.MinCost, // fast tests
		now:        func() time.Time { return fixedNow },
	}
	return svc, users, refresh
}

func mustStatus(t *testing.T, err error, want int) {
	t.Helper()
	appErr, ok := apperror.As(err)
	if !ok {
		t.Fatalf("error is not *apperror.Error: %v", err)
	}
	if appErr.Status != want {
		t.Fatalf("status = %d, want %d (%v)", appErr.Status, want, err)
	}
}

func validRegister() RegisterInput {
	return RegisterInput{Username: "alice", Email: "alice@example.com", Password: "s3cretpw"}
}

func TestRegister_Success(t *testing.T) {
	svc, _, _ := newAuthSvc()

	user, err := svc.Register(context.Background(), validRegister())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.ID == 0 {
		t.Error("expected a persisted user ID")
	}
	if user.PasswordHash == "" || user.PasswordHash == "s3cretpw" {
		t.Error("password must be hashed, not stored as plaintext")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("s3cretpw")) != nil {
		t.Error("stored hash does not verify against the original password")
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	svc, _, _ := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())

	in := validRegister()
	in.Email = "other@example.com" // same username, different email
	_, err := svc.Register(context.Background(), in)
	mustStatus(t, err, 409)
}

func TestRegister_DuplicateEmail(t *testing.T) {
	svc, _, _ := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())

	in := validRegister()
	in.Username = "bob" // same email, different username
	_, err := svc.Register(context.Background(), in)
	mustStatus(t, err, 409)
}

func TestRegister_Validation(t *testing.T) {
	svc, _, _ := newAuthSvc()
	cases := map[string]RegisterInput{
		"short username": {Username: "ab", Email: "a@b.com", Password: "s3cretpw"},
		"bad email":      {Username: "alice", Email: "no-at", Password: "s3cretpw"},
		"short password": {Username: "alice", Email: "a@b.com", Password: "short"},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Register(context.Background(), in)
			mustStatus(t, err, 400)
		})
	}
}

func TestLogin_Success(t *testing.T) {
	svc, _, refresh := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())

	pair, err := svc.Login(context.Background(), LoginInput{Email: "alice@example.com", Password: "s3cretpw"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Error("expected non-empty access and refresh tokens")
	}
	if pair.TokenType != "Bearer" {
		t.Errorf("TokenType = %q, want Bearer", pair.TokenType)
	}
	// Refresh token stored hashed, never raw.
	if _, ok := refresh.byHash[pair.RefreshToken]; ok {
		t.Error("raw refresh token must not be stored; only its hash")
	}
	if _, ok := refresh.byHash[hashToken(pair.RefreshToken)]; !ok {
		t.Error("refresh token hash was not persisted")
	}
}

func TestLogin_Failures(t *testing.T) {
	svc, _, _ := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())

	t.Run("unknown email", func(t *testing.T) {
		_, err := svc.Login(context.Background(), LoginInput{Email: "nobody@example.com", Password: "s3cretpw"})
		mustStatus(t, err, 401)
	})
	t.Run("wrong password", func(t *testing.T) {
		_, err := svc.Login(context.Background(), LoginInput{Email: "alice@example.com", Password: "wrongpass"})
		mustStatus(t, err, 401)
	})
}

func TestRefresh_RotatesAndRevokesOld(t *testing.T) {
	svc, _, refresh := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())
	pair, _ := svc.Login(context.Background(), LoginInput{Email: "alice@example.com", Password: "s3cretpw"})

	oldHash := hashToken(pair.RefreshToken)
	newPair, err := svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if newPair.RefreshToken == pair.RefreshToken {
		t.Error("refresh must rotate the token")
	}
	if refresh.byHash[oldHash].RevokedAt == nil {
		t.Error("old refresh token must be revoked after rotation")
	}
}

func TestRefresh_ReusedTokenRejected(t *testing.T) {
	svc, _, _ := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())
	pair, _ := svc.Login(context.Background(), LoginInput{Email: "alice@example.com", Password: "s3cretpw"})

	// First refresh rotates (revokes) the presented token.
	if _, err := svc.Refresh(context.Background(), pair.RefreshToken); err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	// Reusing the same (now-revoked) token must be rejected.
	_, err := svc.Refresh(context.Background(), pair.RefreshToken)
	mustStatus(t, err, 401)
}

func TestRegister_PasswordTooLong(t *testing.T) {
	svc, _, _ := newAuthSvc()
	in := validRegister()
	in.Password = strings.Repeat("a", 73) // exceeds bcrypt's 72-byte limit
	_, err := svc.Register(context.Background(), in)
	mustStatus(t, err, 400)
}

func TestRefresh_Invalid(t *testing.T) {
	svc, _, refresh := newAuthSvc()

	t.Run("unknown token", func(t *testing.T) {
		_, err := svc.Refresh(context.Background(), "does-not-exist")
		mustStatus(t, err, 401)
	})

	t.Run("revoked token", func(t *testing.T) {
		raw := "revoked-raw"
		revokedAt := fixedNow
		_, _ = refresh.Create(context.Background(), &repository.RefreshToken{
			UserID: 1, TokenHash: hashToken(raw),
			ExpiresAt: fixedNow.Add(time.Hour), RevokedAt: &revokedAt,
		})
		_, err := svc.Refresh(context.Background(), raw)
		mustStatus(t, err, 401)
	})

	t.Run("expired token", func(t *testing.T) {
		raw := "expired-raw"
		_, _ = refresh.Create(context.Background(), &repository.RefreshToken{
			UserID: 1, TokenHash: hashToken(raw),
			ExpiresAt: fixedNow.Add(-time.Hour),
		})
		_, err := svc.Refresh(context.Background(), raw)
		mustStatus(t, err, 401)
	})
}

func TestLogout(t *testing.T) {
	svc, _, refresh := newAuthSvc()
	_, _ = svc.Register(context.Background(), validRegister())
	pair, _ := svc.Login(context.Background(), LoginInput{Email: "alice@example.com", Password: "s3cretpw"})

	if err := svc.Logout(context.Background(), pair.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if refresh.byHash[hashToken(pair.RefreshToken)].RevokedAt == nil {
		t.Error("logout must revoke the refresh token")
	}

	// Idempotent / no enumeration: unknown token is not an error.
	if err := svc.Logout(context.Background(), "unknown-token"); err != nil {
		t.Errorf("logout of unknown token should be a no-op, got %v", err)
	}
}
