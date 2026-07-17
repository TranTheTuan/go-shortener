# Phase 02 — Handler layer

**Status:** pending  
**Priority:** high  
**Effort:** small  
**Blocked by:** Phase 01

## Context

- Spec: [brainstorm-260717-1604-change-subscription-interval.md](../reports/brainstorm-260717-1604-change-subscription-interval.md)
- Handler: `internal/handler/subscription_handler.go:79-112`

## Files to modify

- `internal/handler/subscription_handler.go`

## Implementation steps

1. Add `Interval` to the request struct:
   ```go
   type upgradeRequest struct {
       PlanID   int64  `json:"plan_id"`
       Interval string `json:"interval"` // "month" | "year"
   }
   ```

2. Update the bind guard to require both fields:
   ```go
   if err := c.Bind(&req); err != nil || req.PlanID == 0 || req.Interval == "" {
       return response.Error(c, apperror.New(http.StatusBadRequest, "BAD_REQUEST", "plan_id and interval are required"))
   }
   ```

3. Update the service call:
   ```go
   if err := h.billing.ChangeSubscription(c.Request().Context(), userID, req.PlanID, req.Interval); err != nil {
       return response.Error(c, err)
   }
   ```

4. Update the Swagger comment on `Upgrade` to reflect broader semantics:
   ```go
   // @Summary      Change active subscription plan tier and/or billing interval
   ```

## Todo

- [ ] Add `Interval` field to `upgradeRequest`
- [ ] Update bind guard
- [ ] Update service call to `ChangeSubscription`
- [ ] Update Swagger summary
- [ ] Compile: `go build ./...`

## Success criteria

`go build ./...` clean. `POST /api/subscription/upgrade` with `{"plan_id":3,"interval":"year"}` compiles and routes correctly.
