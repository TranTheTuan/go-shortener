package middleware

import (
	"errors"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/TranTheTuan/go-shortener/pkg/metrics"
)

// Metrics records per-request RED metrics (duration histogram + in-flight gauge)
// via OpenTelemetry. The route label is the Echo template (c.Path()), so path
// params (:code, :id) collapse into one low-cardinality series.
//
// Status is read after next() returns: handlers here write their response
// synchronously (via response.Success/Error) rather than deferring to Echo's
// error handler, so c.Response().Status is final at this point.
func Metrics() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			ctx := c.Request().Context()
			method := c.Request().Method
			start := time.Now()
			// Deferred so the gauge is decremented even if the handler panics
			// (Recover wraps this middleware from the outside).
			defer metrics.TrackInflight(ctx, method)()

			err := next(c)

			route := c.Path()
			if route == "" {
				route = "unmatched" // no route matched (404) — avoid an empty label
			}
			// App handlers write their response synchronously (status is final
			// here). Framework handlers (static/swagger/404) instead RETURN an
			// echo.HTTPError that Echo writes later — read its code so those
			// aren't miscounted as 200.
			status := c.Response().Status
			var he *echo.HTTPError
			if errors.As(err, &he) {
				status = he.Code
			}
			if status == 0 {
				status = 200
			}
			metrics.RecordHTTP(ctx, method, route, status, time.Since(start).Seconds())
			return err
		}
	}
}
