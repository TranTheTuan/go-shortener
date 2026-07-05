package main

import (
	"context"
	"errors"
	"testing"

	"github.com/TranTheTuan/go-shortener/internal/events"
	"github.com/TranTheTuan/go-shortener/internal/repository"
)

type stubRelayRepo struct {
	jobIDs []int64
	err    error
}

func (r *stubRelayRepo) CreateWithOutbox(_ context.Context, job *repository.BulkJob) (*repository.BulkJob, error) {
	return job, nil
}
func (r *stubRelayRepo) GetByID(_ context.Context, _ int64) (*repository.BulkJob, error) {
	return nil, nil
}
func (r *stubRelayRepo) ListByOwner(_ context.Context, _ int64, _, _ int) ([]*repository.BulkJob, error) {
	return nil, nil
}
func (r *stubRelayRepo) UpdateStatus(_ context.Context, _ int64, _ string, _ int) error { return nil }
func (r *stubRelayRepo) UpdateResult(_ context.Context, _ int64, _ string, _ int) error { return nil }
func (r *stubRelayRepo) RelayOutbox(_ context.Context, fn func(int64) error) error {
	if r.err != nil {
		return r.err
	}
	for _, id := range r.jobIDs {
		if err := fn(id); err != nil {
			return err
		}
	}
	return nil
}

type stubBulkProducer struct {
	published []int64
	err       error
}

func (p *stubBulkProducer) Publish(_ context.Context, ev events.BulkJobEvent) error {
	if p.err != nil {
		return p.err
	}
	p.published = append(p.published, ev.JobID)
	return nil
}
func (p *stubBulkProducer) Close() {}

func TestRelayOnce_PublishesAll(t *testing.T) {
	repo := &stubRelayRepo{jobIDs: []int64{1, 2, 3}}
	prod := &stubBulkProducer{}
	relayOnce(context.Background(), repo, prod)
	if len(prod.published) != 3 {
		t.Fatalf("want 3 published, got %d", len(prod.published))
	}
}

func TestRelayOnce_RepoError_DoesNotPanic(t *testing.T) {
	repo := &stubRelayRepo{err: errors.New("db down")}
	prod := &stubBulkProducer{}
	// Must not panic; errors are only logged.
	relayOnce(context.Background(), repo, prod)
	if len(prod.published) != 0 {
		t.Fatal("nothing should be published on repo error")
	}
}

func TestRelayOnce_ProducerError_DoesNotPanic(t *testing.T) {
	repo := &stubRelayRepo{jobIDs: []int64{7}}
	prod := &stubBulkProducer{err: errors.New("kafka down")}
	relayOnce(context.Background(), repo, prod)
	// producer error is returned to RelayOutbox which rolls back — no panic
}
