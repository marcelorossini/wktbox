//go:build linux

package loopback

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

func listenNamespaceLoopback(
	pid int,
	port uint16,
) (listeners []net.Listener, resultErr error) {
	if pid <= 0 {
		return nil, fmt.Errorf("network namespace PID must be positive, got %d", pid)
	}
	if port == 0 {
		return nil, errors.New("network namespace port must be positive")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	original, err := os.Open("/proc/thread-self/ns/net")
	if err != nil {
		return nil, fmt.Errorf("open current network namespace: %w", err)
	}
	defer original.Close()
	target, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
	if err != nil {
		return nil, fmt.Errorf("open workload network namespace for PID %d: %w", pid, err)
	}
	defer target.Close()
	if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf("enter workload network namespace for PID %d: %w", pid, err)
	}

	listeners = make([]net.Listener, 0, 2)
	restore := func() error {
		return unix.Setns(int(original.Fd()), unix.CLONE_NEWNET)
	}
	insideTarget := true
	defer func() {
		if !insideTarget {
			return
		}
		if err := restore(); err != nil {
			closeListeners(listeners)
			listeners = nil
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("restore current network namespace: %w", err),
			)
		}
	}()

	addresses := []struct {
		network string
		host    string
	}{
		{network: "tcp4", host: "127.0.0.1"},
		{network: "tcp6", host: "::1"},
	}
	for _, address := range addresses {
		listener, err := (&net.ListenConfig{}).Listen(
			context.Background(),
			address.network,
			net.JoinHostPort(address.host, strconv.Itoa(int(port))),
		)
		if err != nil {
			closeListeners(listeners)
			return nil, fmt.Errorf(
				"bind workload localhost:%d in PID %d: %w",
				port,
				pid,
				err,
			)
		}
		listeners = append(listeners, listener)
	}
	if err := restore(); err != nil {
		closeListeners(listeners)
		return nil, fmt.Errorf("restore current network namespace: %w", err)
	}
	insideTarget = false
	return listeners, nil
}
