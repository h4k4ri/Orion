package events

import "context"

// NoopPublisher discards all events. Kept only for isolated tests and tooling.
type NoopPublisher struct{}

func (NoopPublisher) PublishTask(_ context.Context, _ TaskEvent) error { return nil }
func (NoopPublisher) PublishResource(_ context.Context, _ ResourceEvent) error { return nil }
