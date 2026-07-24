package loopback

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"

	"wktbox/internal/portforward"
)

type PublishRoute struct {
	Name          string
	ListenAddress string
	TargetAddress string
}

type PublishServer struct {
	routes      []PublishRoute
	dialContext func(context.Context, string, string) (net.Conn, error)
	mutex       sync.Mutex
	listeners   []net.Listener
}

func NewPublishServer(routes []PublishRoute) *PublishServer {
	dialer := &net.Dialer{}
	return &PublishServer{
		routes:      append([]PublishRoute(nil), routes...),
		dialContext: dialer.DialContext,
	}
}

func (server *PublishServer) Listen(ctx context.Context) ([]net.Listener, error) {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	if len(server.listeners) != 0 {
		return nil, errors.New("publication server is already listening")
	}
	listeners := make([]net.Listener, 0, len(server.routes))
	for _, route := range server.routes {
		if route.Name == "" || route.ListenAddress == "" || route.TargetAddress == "" {
			closeListeners(listeners)
			return nil, errors.New(
				"publication route name, listener, and target are required",
			)
		}
		listener, err := (&net.ListenConfig{}).Listen(
			ctx,
			"tcp",
			route.ListenAddress,
		)
		if err != nil {
			closeListeners(listeners)
			return nil, fmt.Errorf(
				"bind publication %q on %s: %w",
				route.Name,
				route.ListenAddress,
				err,
			)
		}
		listeners = append(listeners, listener)
	}
	server.listeners = listeners
	return append([]net.Listener(nil), listeners...), nil
}

func (server *PublishServer) Serve(
	ctx context.Context,
	listeners []net.Listener,
) error {
	if len(listeners) != len(server.routes) {
		return fmt.Errorf(
			"publication listener count %d does not match route count %d",
			len(listeners),
			len(server.routes),
		)
	}
	go func() {
		<-ctx.Done()
		_ = server.Close()
	}()
	failures := make(chan error, len(listeners))
	var wait sync.WaitGroup
	for index, listener := range listeners {
		route := server.routes[index]
		wait.Add(1)
		go func() {
			defer wait.Done()
			if err := server.serveListener(ctx, listener, route); err != nil {
				failures <- err
			}
		}()
	}
	wait.Wait()
	close(failures)
	var result error
	for err := range failures {
		result = errors.Join(result, err)
	}
	return result
}

func (server *PublishServer) Close() error {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	var result error
	for _, listener := range server.listeners {
		err := listener.Close()
		if !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	server.listeners = nil
	return result
}

func (server *PublishServer) ListenerCount() int {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return len(server.listeners)
}

func (server *PublishServer) serveListener(
	ctx context.Context,
	listener net.Listener,
	route PublishRoute,
) error {
	for {
		source, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf(
				"accept publication %q on %s: %w",
				route.Name,
				route.ListenAddress,
				err,
			)
		}
		go server.forward(ctx, source, route)
	}
}

func (server *PublishServer) forward(
	ctx context.Context,
	source net.Conn,
	route PublishRoute,
) {
	defer source.Close()
	target, err := server.dialContext(ctx, "tcp", route.TargetAddress)
	if err != nil {
		return
	}
	defer target.Close()
	portforward.BridgeTCP(source, target)
}

func RunPublishServer(
	ctx context.Context,
	mappings []portforward.Mapping,
	targetHost string,
) error {
	routes := make([]PublishRoute, 0)
	for _, mapping := range portforward.Filter(mappings, portforward.Publish) {
		routes = append(routes, PublishRoute{
			Name: mapping.Name,
			ListenAddress: net.JoinHostPort(
				"0.0.0.0",
				strconv.Itoa(int(mapping.SourcePort)),
			),
			TargetAddress: net.JoinHostPort(
				targetHost,
				strconv.Itoa(int(mapping.TargetPort)),
			),
		})
	}
	if len(routes) == 0 {
		return errors.New("publication configuration contains no routes")
	}
	server := NewPublishServer(routes)
	listeners, err := server.Listen(ctx)
	if err != nil {
		return err
	}
	defer server.Close()
	return server.Serve(ctx, listeners)
}
