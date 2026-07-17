package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/TranTheTuan/go-shortener/internal/repository"
)

// basicPlanRepo returns a plan repo seeded with the default "basic" plan.
func basicPlanRepo() *mockPlanRepo {
	plan := &repository.Plan{ID: 1, Code: "basic", DailyLinkQuota: 10}
	return &mockPlanRepo{
		byCode: map[string]*repository.Plan{"basic": plan},
		byID:   map[int64]*repository.Plan{1: plan},
	}
}

// newUserSvc builds a userService with a basic-plan repo for provisioning.
func newUserSvc() (*mockUserRepo, UserService) {
	repo := newMockUserRepo()
	return repo, NewUserService(repo, basicPlanRepo(), "basic", "1.0")
}

func TestSyncFromKeycloak_CreatesOnFirstSight(t *testing.T) {
	repo, svc := newUserSvc()

	u, err := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "a@b.com", Username: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u.ID == 0 {
		t.Error("expected a persisted local id")
	}
	if u.KeycloakSub == nil || *u.KeycloakSub != "kc-1" {
		t.Errorf("keycloak_sub = %v, want kc-1", u.KeycloakSub)
	}
	if u.Email != "a@b.com" || u.Username != "alice" {
		t.Errorf("claims not stored: %+v", u)
	}
	// A basic subscription must be provisioned atomically with the user.
	sub := repo.subs[u.ID]
	if sub == nil {
		t.Fatal("expected a subscription created for the new user")
	}
	if sub.PlanID != 1 || sub.Status != "active" {
		t.Errorf("subscription = %+v, want basic plan id 1, status active", sub)
	}
}

func TestSyncFromKeycloak_ReturnsExisting(t *testing.T) {
	_, svc := newUserSvc()

	first, _ := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "a@b.com", Username: "alice"})
	second, err := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "a@b.com", Username: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("same sub must map to the same local id: %d vs %d", second.ID, first.ID)
	}
}

func TestSyncFromKeycloak_RefreshesChangedClaims(t *testing.T) {
	_, svc := newUserSvc()

	created, _ := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "old@b.com", Username: "alice"})
	updated, err := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "new@b.com", Username: "alice2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.ID != created.ID {
		t.Errorf("id changed on update: %d vs %d", updated.ID, created.ID)
	}
	if updated.Email != "new@b.com" || updated.Username != "alice2" {
		t.Errorf("claims not refreshed: %+v", updated)
	}
}

func TestSyncFromKeycloak_DistinctSubsDistinctUsers(t *testing.T) {
	_, svc := newUserSvc()

	a, _ := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "a@b.com", Username: "alice"})
	b, _ := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-2", Email: "b@b.com", Username: "bob"})
	if a.ID == b.ID {
		t.Errorf("distinct subs must map to distinct users: both %d", a.ID)
	}
}

func TestSyncFromKeycloak_RejectsEmptyClaims(t *testing.T) {
	_, svc := newUserSvc()
	cases := map[string]SyncInput{
		"empty sub":      {Sub: "", Email: "a@b.com", Username: "alice"},
		"empty email":    {Sub: "kc-1", Email: "", Username: "alice"},
		"empty username": {Sub: "kc-1", Email: "a@b.com", Username: ""},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.SyncFromKeycloak(context.Background(), in)
			wantStatus(t, err, http.StatusUnauthorized)
		})
	}
}

func TestAcceptTerms_VersionMismatch(t *testing.T) {
	repo, svc := newUserSvc()

	// Create a user first
	u, _ := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "a@b.com", Username: "alice"})

	// Try to accept a different version
	err := svc.AcceptTerms(context.Background(), u.ID, "2.0")
	wantStatus(t, err, http.StatusBadRequest)

	// Verify terms were NOT accepted
	updated, _ := repo.GetByID(context.Background(), u.ID)
	if updated.TermsVersion != nil {
		t.Errorf("terms should not be accepted, but got version %s", *updated.TermsVersion)
	}
}

func TestAcceptTerms_Success(t *testing.T) {
	repo, svc := newUserSvc()

	// Create a user first
	u, _ := svc.SyncFromKeycloak(context.Background(), SyncInput{Sub: "kc-1", Email: "a@b.com", Username: "alice"})

	// Accept terms with correct version
	err := svc.AcceptTerms(context.Background(), u.ID, "1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify terms were accepted
	updated, _ := repo.GetByID(context.Background(), u.ID)
	if updated.TermsVersion == nil || *updated.TermsVersion != "1.0" {
		t.Errorf("terms version = %v, want 1.0", updated.TermsVersion)
	}
	if updated.TermsAcceptedAt == nil {
		t.Error("expected terms_accepted_at to be set")
	}
}

func TestAcceptTerms_UserNotFound(t *testing.T) {
	_, svc := newUserSvc()

	// Try to accept terms for non-existent user
	err := svc.AcceptTerms(context.Background(), 999, "1.0")
	if err == nil {
		t.Fatal("expected an error for non-existent user")
	}
	if !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// sanity: mockUserRepo satisfies the repository interface used by the service.
var _ repository.UserRepository = (*mockUserRepo)(nil)
