package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// newTestLogger returns a logger writing JSON into buf, wrapped with TraceHandler.
func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(NewTraceHandler(slog.NewJSONHandler(buf, nil)))
}

func TestTraceHandler_StampsIDsWhenSpanPresent(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	tid, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sid, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled,
	}))

	log.InfoContext(ctx, "hello")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("bad log json: %v", err)
	}
	if m["trace_id"] != tid.String() {
		t.Fatalf("trace_id = %v, want %s", m["trace_id"], tid)
	}
	if m["span_id"] != sid.String() {
		t.Fatalf("span_id = %v, want %s", m["span_id"], sid)
	}
}

func TestTraceHandler_NoIDsWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf)

	log.InfoContext(context.Background(), "hello")

	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("bad log json: %v", err)
	}
	if _, ok := m["trace_id"]; ok {
		t.Fatal("trace_id must be absent when no span is in context")
	}
}

// WithAttrs must preserve trace stamping (embedding alone would drop it).
func TestTraceHandler_WithAttrsKeepsStamping(t *testing.T) {
	var buf bytes.Buffer
	log := newTestLogger(&buf).With("component", "x")

	tid, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	sid, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled,
	}))
	log.InfoContext(ctx, "hello")

	var m map[string]any
	_ = json.Unmarshal(buf.Bytes(), &m)
	if m["trace_id"] != tid.String() {
		t.Fatalf("trace_id lost after With(): got %v", m["trace_id"])
	}
}
