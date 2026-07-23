package loopback

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

type listenerPair struct {
	ipv4 net.Listener
	ipv6 net.Listener
}

func bindPair(
	listen func(network string, address string) (net.Listener, error),
	port uint16,
) (*listenerPair, error) {
	value := strconv.Itoa(int(port))
	ipv4, err := listen("tcp4", net.JoinHostPort("127.0.0.1", value))
	if err != nil {
		return nil, err
	}
	ipv6, err := listen("tcp6", net.JoinHostPort("::1", value))
	if err != nil {
		_ = ipv4.Close()
		return nil, err
	}
	return &listenerPair{ipv4: ipv4, ipv6: ipv6}, nil
}

func (pair *listenerPair) close() {
	_ = pair.ipv4.Close()
	_ = pair.ipv6.Close()
}

func proxyConnection(
	ctx context.Context,
	downstream net.Conn,
	target string,
	dialContext func(context.Context, string, string) (net.Conn, error),
) {
	defer downstream.Close()
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	upstream, err := dialContext(dialCtx, "tcp", target)
	cancel()
	if err != nil {
		return
	}
	defer upstream.Close()

	finished := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = downstream.Close()
			_ = upstream.Close()
		case <-finished:
		}
	}()

	var copies sync.WaitGroup
	copies.Add(2)
	go copyHalf(&copies, upstream, downstream)
	go copyHalf(&copies, downstream, upstream)
	copies.Wait()
	close(finished)
}

func copyHalf(group *sync.WaitGroup, destination net.Conn, source net.Conn) {
	defer group.Done()
	_, _ = io.Copy(destination, source)
	if writer, ok := destination.(interface{ CloseWrite() error }); ok {
		_ = writer.CloseWrite()
	}
}

func listenerClosed(err error) bool {
	return errors.Is(err, net.ErrClosed)
}
