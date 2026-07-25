//go:build !linux

package loopback

import (
	"context"
	"errors"
	"net"
)

func dialNamespaceContext(
	context.Context,
	int,
	string,
	string,
) (net.Conn, error) {
	return nil, errors.New(
		"DinD published-port dialing requires Linux containers",
	)
}

func listenNamespaceLoopback(int, uint16) ([]net.Listener, error) {
	return nil, errors.New("workload localhost imports require Linux containers")
}
