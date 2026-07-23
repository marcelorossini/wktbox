package loopback

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sync"
	"time"
)

type Backoff struct {
	Initial time.Duration
	Maximum time.Duration
	Jitter  float64
}

type DaemonOptions struct {
	Source         Source
	Reconciler     *Reconciler
	Debounce       time.Duration
	ResyncInterval time.Duration
	Backoff        Backoff
	Random         func() float64
	Logf           func(string, ...any)
	Now            func() time.Time
}

type Daemon struct {
	source         Source
	reconciler     *Reconciler
	debounce       time.Duration
	resyncInterval time.Duration
	backoff        Backoff
	random         func() float64
	logf           func(string, ...any)
	now            func() time.Time
	syncMutex      sync.Mutex
	logMutex       sync.Mutex
	lastLog        map[string]time.Time
}

func NewDaemon(options DaemonOptions) *Daemon {
	if options.Debounce <= 0 {
		options.Debounce = 100 * time.Millisecond
	}
	if options.ResyncInterval <= 0 {
		options.ResyncInterval = 30 * time.Second
	}
	if options.Backoff.Initial <= 0 {
		options.Backoff.Initial = 250 * time.Millisecond
	}
	if options.Backoff.Maximum <= 0 {
		options.Backoff.Maximum = 5 * time.Second
	}
	if options.Backoff.Jitter < 0 {
		options.Backoff.Jitter = 0
	}
	if options.Backoff.Jitter == 0 {
		options.Backoff.Jitter = 0.20
	}
	if options.Random == nil {
		options.Random = rand.Float64
	}
	if options.Logf == nil {
		options.Logf = func(string, ...any) {}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Daemon{
		source:         options.Source,
		reconciler:     options.Reconciler,
		debounce:       options.Debounce,
		resyncInterval: options.ResyncInterval,
		backoff:        options.Backoff,
		random:         options.Random,
		logf:           options.Logf,
		now:            options.Now,
		lastLog:        make(map[string]time.Time),
	}
}

func (daemon *Daemon) Run(ctx context.Context) (runErr error) {
	if daemon.source == nil {
		return errors.New("loopback source is required")
	}
	if daemon.reconciler == nil {
		return errors.New("loopback reconciler is required")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		runErr = errors.Join(runErr, daemon.reconciler.Close(closeCtx), daemon.source.Close())
	}()

	delay := daemon.backoff.Initial
	for {
		if ctx.Err() != nil {
			return nil
		}
		streamCtx, cancelStream := context.WithCancel(ctx)
		subscription, err := daemon.source.Subscribe(streamCtx)
		if err != nil {
			cancelStream()
			daemon.setStream(EventStreamDisconnected)
			daemon.logLimited("subscribe", "loopback event subscription failed: %v", err)
			if !waitContext(ctx, daemon.jittered(delay)) {
				return nil
			}
			delay = daemon.nextDelay(delay)
			continue
		}

		daemon.setStream(EventStreamConnected)
		if _, err := daemon.Sync(ctx); err != nil {
			cancelStream()
			daemon.setStream(EventStreamDisconnected)
			daemon.logLimited("snapshot", "loopback full sync failed: %v", err)
			if !waitContext(ctx, daemon.jittered(delay)) {
				return nil
			}
			delay = daemon.nextDelay(delay)
			continue
		}
		delay = daemon.backoff.Initial

		streamErr := daemon.consume(streamCtx, subscription)
		cancelStream()
		if ctx.Err() != nil {
			return nil
		}
		daemon.setStream(EventStreamDisconnected)
		daemon.logLimited("events", "loopback event stream disconnected: %v", streamErr)
		if !waitContext(ctx, daemon.jittered(delay)) {
			return nil
		}
		delay = daemon.nextDelay(delay)
	}
}

func (daemon *Daemon) Sync(ctx context.Context) (Status, error) {
	daemon.syncMutex.Lock()
	defer daemon.syncMutex.Unlock()

	containers, err := daemon.source.Snapshot(ctx)
	if err != nil {
		return daemon.Status(), fmt.Errorf("snapshot inner Docker containers: %w", err)
	}
	status := daemon.reconciler.Apply(ctx, Discover(containers))
	stream := status.EventStream
	if stream == "" {
		stream = EventStreamDisconnected
	}
	return daemon.reconciler.SetEventStream(stream), nil
}

func (daemon *Daemon) Status() Status {
	if daemon.reconciler == nil {
		return Unavailable(errors.New("loopback reconciler is not configured"))
	}
	return daemon.reconciler.Status()
}

func (daemon *Daemon) consume(ctx context.Context, subscription Subscription) error {
	resync := time.NewTicker(daemon.resyncInterval)
	defer resync.Stop()

	var debounce *time.Timer
	var debounceChannel <-chan time.Time
	stopDebounce := func() {
		if debounce != nil && !debounce.Stop() {
			select {
			case <-debounce.C:
			default:
			}
		}
	}
	defer stopDebounce()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case event, open := <-subscription.Events:
			if !open {
				return io.EOF
			}
			if !relevantEvent(event.Action) {
				continue
			}
			if debounce == nil {
				debounce = time.NewTimer(daemon.debounce)
			} else {
				stopDebounce()
				debounce.Reset(daemon.debounce)
			}
			debounceChannel = debounce.C
		case streamErr, open := <-subscription.Errors:
			if !open || streamErr == nil {
				return io.EOF
			}
			return streamErr
		case <-debounceChannel:
			debounceChannel = nil
			if _, err := daemon.Sync(ctx); err != nil {
				daemon.logLimited("snapshot", "loopback event sync failed: %v", err)
			}
		case <-resync.C:
			if _, err := daemon.Sync(ctx); err != nil {
				daemon.logLimited("snapshot", "loopback periodic sync failed: %v", err)
			}
		}
	}
}

func (daemon *Daemon) setStream(state string) {
	daemon.reconciler.SetEventStream(state)
}

func (daemon *Daemon) nextDelay(current time.Duration) time.Duration {
	next := current * 2
	if next > daemon.backoff.Maximum {
		return daemon.backoff.Maximum
	}
	return next
}

func (daemon *Daemon) jittered(value time.Duration) time.Duration {
	factor := 1 + ((daemon.random()*2)-1)*daemon.backoff.Jitter
	return time.Duration(float64(value) * factor)
}

func (daemon *Daemon) logLimited(class string, format string, values ...any) {
	daemon.logMutex.Lock()
	defer daemon.logMutex.Unlock()
	now := daemon.now()
	if last := daemon.lastLog[class]; !last.IsZero() && now.Sub(last) < 30*time.Second {
		return
	}
	daemon.lastLog[class] = now
	daemon.logf(format, values...)
}

func relevantEvent(action string) bool {
	switch action {
	case "start", "die", "stop", "destroy", "rename":
		return true
	default:
		return false
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
