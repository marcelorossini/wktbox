//go:build !linux

package loopback

import (
	"errors"
	"net"
)

func listenNamespaceLoopback(int, uint16) ([]net.Listener, error) {
	return nil, errors.New("workload localhost imports require Linux containers")
}
