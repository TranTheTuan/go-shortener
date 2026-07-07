# Phase 06 — Tests

## Overview
- **Priority:** P2 · **Status:** done · Depends on: 01–03
- Verify instruments record correctly and `/metrics` exposes them. Mock-free where possible using OTel's in-memory reader.

## Related files
- Create: `pkg/metrics/metrics_test.go`
- Create/extend: `internal/middleware/metrics_test.go`

## Cases
**pkg/metrics**
- Setup with a `sdkmetric.NewManualReader()` (test provider) instead of the Prometheus exporter; after calling record helpers (`RecordRedirect("ok")`, `RecordCacheLookup(true)`, `RecordQuotaRejection`, `RecordClickEvent("dropped")`), `Collect` and assert the expected metric + attribute set + value.
- Assert label values are the bounded enums (no unexpected attributes).

**HTTP middleware**
- Wrap a dummy Echo handler with `Metrics()`, serve a request against a route with a param (`/:code`), collect from the manual reader, assert:
  - one duration observation with `http_route` = the TEMPLATE (`/:code`), not the raw path.
  - `http.status_code` recorded; in-flight returns to 0 after the request.

**/metrics endpoint (optional integration)**
- Build the Prometheus registry via the real exporter, record something, `promhttp` handler responds 200 with the metric name present. Keep lightweight.

## Todo
- [x] pkg/metrics record helpers (manual reader assertions)
- [x] middleware route-template + status + in-flight test
- [x] optional /metrics handler smoke test
- [x] `make test` green (-race); existing suite unaffected

## Success criteria
- Instruments emit the documented names/labels/values.
- Route label proven to be the template (guards the cardinality rule).
- No real Prometheus/Redis needed for unit tests.

## Notes
- Use OTel `sdkmetric.NewManualReader` + `rdr.Collect(ctx, &rm)` to inspect metrics deterministically — the idiomatic way to unit-test OTel instruments.
