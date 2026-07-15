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

// subscriptionPayload is the response body for GET /api/subscription.
type subscriptionPayload struct {
	Plan           any `json:"plan"`
	Subscription   any `json:"subscription"`
	QuotaRemaining int `json:"quota_remaining"`
}

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
//
// @Summary      List available plans
// @Tags         billing
// @Produce      json
// @Success      200  {array}   github_com_TranTheTuan_go-shortener_internal_repository.Plan
// @Failure      500  {object}  response.Envelope
// @Router       /api/plans [get]
func (h *SubscriptionHandler) Plans(c echo.Context) error {
	list, err := h.plans.List(c.Request().Context())
	if err != nil {
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, list)
}

// Get handles GET /api/subscription.
//
// @Summary      Get current subscription and quota
// @Tags         billing
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  response.Envelope{data=handler.subscriptionPayload}
// @Failure      401  {object}  response.Envelope
// @Failure      500  {object}  response.Envelope
// @Router       /api/subscription [get]
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

	return response.Success(c, http.StatusOK, subscriptionPayload{
		Plan:           plan,
		Subscription:   sub,
		QuotaRemaining: remaining,
	})
}

// portalURLPayload is the response body for GET /api/subscription/portal.
type portalURLPayload struct {
	URL string `json:"url"`
}

// Portal handles GET /api/subscription/portal.
// Returns the Paddle Customer Portal URL as JSON so the frontend can navigate
// directly (window.open / window.location.href). A server-side redirect is not
// used because fetch() cannot follow cross-origin redirects to the Paddle portal
// domain — the browser blocks the response due to missing CORS headers on the
// Paddle side.
//
// @Summary      Get Paddle Customer Portal URL
// @Tags         billing
// @Security     BearerAuth
// @Success      200  {object}  response.Envelope{data=handler.portalURLPayload}
// @Failure      401  {object}  response.Envelope  "not authenticated"
// @Failure      404  {object}  response.Envelope  "no active Paddle subscription"
// @Failure      500  {object}  response.Envelope  "could not generate portal URL"
// @Router       /api/subscription/portal [get]
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
		return response.Error(c, err)
	}
	return response.Success(c, http.StatusOK, portalURLPayload{URL: url})
}
