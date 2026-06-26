package middleware

import (
	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/internal/service"
)

// CtxLinkReused is the echo.Context key a handler sets to true when a create
// request reused an existing link (dedup hit) instead of creating a new one.
// QuotaCheck reads it to refund the reserved quota slot.
const CtxLinkReused = "link_reused"

// CtxQuotaExhausted is the echo.Context key QuotaCheck sets when the user is
// over their daily limit. The handler must reject a NEW link with 429 but may
// still return an existing one (reuse never consumes quota). This defers the
// limit decision until after dedup, so an at-limit user re-submitting a URL
// they already shortened still gets their link instead of a spurious 429.
const CtxQuotaExhausted = "quota_exhausted"

// QuotaCheck enforces the per-user daily link quota. It runs after Authn and
// after DuplicateURLCheck on the create route. Requests without a user
// (API-key/unowned) are not subject to per-user quota and pass through.
//
// It reserves a slot up front (atomic INCR in QuotaService). When over the
// limit it does NOT reject outright — it flags the context and lets the handler
// decide (so a dedup can still succeed). A reserved slot is refunded when the
// handler failed or merely reused an existing link.
func QuotaCheck(quota service.QuotaService) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			userID, ok := UserIDFrom(c)
			if !ok {
				return next(c)
			}

			// reserved is true only when a slot was actually consumed (under
			// limit). When over limit, Allow has already balanced its own INCR,
			// so there is nothing to refund here.
			reserved, _ := quota.Allow(c.Request().Context(), userID)
			if !reserved {
				c.Set(CtxQuotaExhausted, true)
			}

			err := next(c)

			if reserved && (c.Response().Status >= 400 || c.Get(CtxLinkReused) == true) {
				quota.Release(c.Request().Context(), userID)
			}
			return err
		}
	}
}
