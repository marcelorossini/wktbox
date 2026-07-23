package loopback

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"
)

type ReconcilerOptions struct {
	Listen      func(network string, address string) (net.Listener, error)
	DialContext func(context.Context, string, string) (net.Conn, error)
	Now         func() time.Time
}

type activeRoute struct {
	publication Publication
	listeners   *listenerPair
}

type Reconciler struct {
	mutex       sync.Mutex
	context     context.Context
	cancel      context.CancelFunc
	listen      func(network string, address string) (net.Listener, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
	now         func() time.Time
	active      map[uint16]*activeRoute
	status      Status
	group       sync.WaitGroup
	closed      bool
}

func NewReconciler(options ReconcilerOptions) *Reconciler {
	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	dialContext := options.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{}
		dialContext = dialer.DialContext
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Reconciler{
		context:     ctx,
		cancel:      cancel,
		listen:      listen,
		dialContext: dialContext,
		now:         now,
		active:      make(map[uint16]*activeRoute),
		status: Status{
			Routes:   []Route{},
			Warnings: []Warning{},
		},
	}
}

func (reconciler *Reconciler) Apply(_ context.Context, desired Desired) Status {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()

	wanted := make(map[uint16]Publication, len(desired.Publications))
	for _, publication := range desired.Publications {
		wanted[publication.Port] = clonePublication(publication)
	}
	for port, route := range reconciler.active {
		if _, exists := wanted[port]; exists {
			continue
		}
		route.listeners.close()
		delete(reconciler.active, port)
	}

	routes := make([]Route, 0, len(wanted))
	for port, publication := range wanted {
		if existing := reconciler.active[port]; existing != nil {
			existing.publication = clonePublication(publication)
			routes = append(routes, routeFromPublication(publication, RouteListening, ""))
			continue
		}
		pair, err := bindPair(reconciler.listen, port)
		if err != nil {
			routes = append(routes, routeFromPublication(publication, RouteConflict, err.Error()))
			continue
		}
		reconciler.active[port] = &activeRoute{
			publication: clonePublication(publication),
			listeners:   pair,
		}
		reconciler.serve(pair.ipv4, publication.Target)
		reconciler.serve(pair.ipv6, publication.Target)
		routes = append(routes, routeFromPublication(publication, RouteListening, ""))
	}
	sort.Slice(routes, func(left, right int) bool {
		return routes[left].Port < routes[right].Port
	})
	reconciler.status = Status{
		EventStream: reconciler.status.EventStream,
		UpdatedAt:   reconciler.now().UTC(),
		Routes:      routes,
		Warnings:    cloneWarnings(desired.Warnings),
	}
	return cloneStatus(reconciler.status)
}

func (reconciler *Reconciler) Status() Status {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()
	return cloneStatus(reconciler.status)
}

func (reconciler *Reconciler) SetEventStream(state string) Status {
	reconciler.mutex.Lock()
	defer reconciler.mutex.Unlock()
	reconciler.status.EventStream = state
	reconciler.status.UpdatedAt = reconciler.now().UTC()
	return cloneStatus(reconciler.status)
}

func (reconciler *Reconciler) Close(ctx context.Context) error {
	reconciler.mutex.Lock()
	if !reconciler.closed {
		reconciler.closed = true
		reconciler.cancel()
		for port, route := range reconciler.active {
			route.listeners.close()
			delete(reconciler.active, port)
		}
	}
	reconciler.mutex.Unlock()

	finished := make(chan struct{})
	go func() {
		reconciler.group.Wait()
		close(finished)
	}()
	select {
	case <-finished:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (reconciler *Reconciler) serve(listener net.Listener, target string) {
	reconciler.group.Add(1)
	go func() {
		defer reconciler.group.Done()
		for {
			connection, err := listener.Accept()
			if err != nil {
				if listenerClosed(err) {
					return
				}
				continue
			}
			reconciler.group.Add(1)
			go func() {
				defer reconciler.group.Done()
				proxyConnection(reconciler.context, connection, target, reconciler.dialContext)
			}()
		}
	}()
}

func routeFromPublication(publication Publication, state string, detail string) Route {
	return Route{
		Port:     publication.Port,
		Target:   publication.Target,
		Sources:  cloneStrings(publication.Sources),
		Protocol: "tcp",
		State:    state,
		Error:    detail,
	}
}

func clonePublication(publication Publication) Publication {
	publication.Sources = cloneStrings(publication.Sources)
	return publication
}

func cloneWarnings(warnings []Warning) []Warning {
	if warnings == nil {
		return nil
	}
	result := make([]Warning, len(warnings))
	copy(result, warnings)
	return result
}

func cloneStatus(status Status) Status {
	if status.Routes != nil {
		routes := make([]Route, len(status.Routes))
		copy(routes, status.Routes)
		status.Routes = routes
	}
	for index := range status.Routes {
		status.Routes[index].Sources = cloneStrings(status.Routes[index].Sources)
	}
	status.Warnings = cloneWarnings(status.Warnings)
	return status
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	result := make([]string, len(values))
	copy(result, values)
	return result
}
