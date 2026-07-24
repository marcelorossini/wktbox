package loopback

import "context"

type Event struct {
	Action string
}

type Subscription struct {
	Events <-chan Event
	Errors <-chan error
}

type Source interface {
	Snapshot(context.Context) ([]Container, error)
	Subscribe(context.Context) (Subscription, error)
	Stop(context.Context, string) error
	Close() error
}
