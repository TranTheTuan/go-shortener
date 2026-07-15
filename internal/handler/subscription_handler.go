package handler

import (
	"net/http"

	"github.com/labstack/echo/v4"

	appmw "github.com/TranTheTuan/go-shortener/internal/middleware"
	"github.com/TranTheTuan/go-shortener/internal/repository"
	"github.com/TranTheTuan/go-shortener/internal/service"
	"github.com/TranTheTuan/go-shortener/pkg/apperror"
	"github.com/TranTheTuan/go-shortener/pkg/response"
)

// SubscriptionHandler exposes billing/subscription endpoints.
type SubscriptionHandler struct {
	billing service.BillingService
	quota   service.QuotaService
	plans   repository.PlanRepository
}

func NewSubscriptionHandler(billing service.BillingService, quota service.QuotaService, plans repository.PlanRepository) *SubscriptionHandler {
	return &SubscriptionHandler{billing: billing, quota: quota, plans: plans}
}

// Plans handles GET /api/plans — public, returns the plan catalog with Paddle price IDs.
func (h *SubscriptionHandler) Plans(c echo.Context) error {
	list, err := h.plans.List(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, list)
}

// Get handles GET /api/subscription.
func (h *SubscriptionHandler) Get(c echo.Context) error {
	userID, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}

	plan, sub, err := h.billing.CurrentPlan(c.Request().Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}

	remaining := h.quota.Remaining(c.Request().Context(), userID)

	type payload struct {
		Plan           any `json:"plan"`
		Subscription   any `json:"subscription"`
		QuotaRemaining int `json:"quota_remaining"`
	}
	return response.Success(c, http.StatusOK, payload{
		Plan:           plan,
		Subscription:   sub,
		QuotaRemaining: remaining,
	})
}

// Portal handles GET /api/subscription/portal.
// Generates a Paddle Customer Portal URL and redirects the user.
func (h *SubscriptionHandler) Portal(c echo.Context) error {
	userID, ok := appmw.UserIDFrom(c)
	if !ok {
		return response.Error(c, apperror.New(http.StatusUnauthorized, "UNAUTHORIZED", "not authenticated"))
	}

	_, sub, err := h.billing.CurrentPlan(c.Request().Context(), userID)
	if err != nil {
		return response.Error(c, err)
	}
	if sub == nil || sub.PaddleCustomerID == nil {
		return response.Error(c, apperror.New(http.StatusNotFound, "NO_SUBSCRIPTION", "no active Paddle subscription found"))
	}

	url, err := h.billing.GeneratePortalURL(c.Request().Context(), *sub.PaddleCustomerID)
	if err != nil {
		return response.Error(c, apperror.New(http.StatusServiceUnavailable, "PORTAL_UNAVAILABLE", "could not generate portal URL"))
	}
	return c.Redirect(http.StatusFound, url)
}
