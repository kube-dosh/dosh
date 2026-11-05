package app

import (
	"context"
	"errors"
	"testing"

	"dosh/internal/domain"
)

type eventsRepository struct {
	events   []domain.Event
	eventsCh chan domain.Event
	errs     chan error
	err      error
}

func (r *eventsRepository) ListEvents(context.Context, domain.ListQuery) ([]domain.Event, error) {
	return r.events, r.err
}
func (r *eventsRepository) WatchEvents(context.Context, string) (<-chan domain.Event, <-chan error, error) {
	if r.err != nil {
		return nil, nil, r.err
	}
	return r.eventsCh, r.errs, nil
}

func TestEventsServiceListsAndWatches(t *testing.T) {
	eventsCh := make(chan domain.Event)
	errs := make(chan error)
	repo := &eventsRepository{events: []domain.Event{{Reason: "BackOff"}}, eventsCh: eventsCh, errs: errs}
	service := New(repo)
	events, err := service.List(context.Background(), domain.ListQuery{Namespace: "prod"})
	if err != nil || len(events) != 1 || events[0].Reason != "BackOff" {
		t.Fatalf("List() = (%#v, %v)", events, err)
	}
	gotEvents, gotErrs, err := service.Watch(context.Background(), "prod")
	if err != nil || gotEvents != eventsCh || gotErrs != errs {
		t.Fatalf("Watch() = (%v, %v, %v)", gotEvents, gotErrs, err)
	}
}

func TestEventsServicePropagatesDependencyFailure(t *testing.T) {
	dependencyErr := errors.New("watch failed")
	service := New(&eventsRepository{err: dependencyErr})
	if _, _, err := service.Watch(context.Background(), "prod"); !errors.Is(err, dependencyErr) {
		t.Fatalf("Watch() error = %v", err)
	}
}
