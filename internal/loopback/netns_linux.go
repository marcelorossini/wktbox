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

func dialNamespaceContext(
	ctx context.Context,
	pid int,
	network string,
	address string,
) (connection net.Conn, resultErr error) {
	if pid <= 0 {
		return nil, fmt.Errorf("network namespace PID must be positive, got %d", pid)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("parse network namespace address %q: %w", address, err)
	}
	if net.ParseIP(host) == nil {
		return nil, fmt.Errorf(
			"network namespace address %q must use a numeric IP address",
			address,
		)
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
		return nil, fmt.Errorf(
			"open target network namespace for PID %d: %w",
			pid,
			err,
		)
	}
	defer target.Close()
	if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
		return nil, fmt.Errorf(
			"enter target network namespace for PID %d: %w",
			pid,
			err,
		)
	}

	restore := func() error {
		return unix.Setns(int(original.Fd()), unix.CLONE_NEWNET)
	}
	insideTarget := true
	defer func() {
		if !insideTarget {
			return
		}
		if err := restore(); err != nil {
			if connection != nil {
				_ = connection.Close()
				connection = nil
			}
			resultErr = errors.Join(
				resultErr,
				fmt.Errorf("restore current network namespace: %w", err),
			)
		}
	}()

	connection, err = (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf(
			"dial %s in network namespace for PID %d: %w",
			address,
			pid,
			err,
		)
	}
	if err := restore(); err != nil {
		_ = connection.Close()
		connection = nil
		return nil, fmt.Errorf("restore current network namespace: %w", err)
	}
	insideTarget = false
	return connection, nil
}

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
		if os.IsNotExist(err) {
			return nil, fmt.Errorf(
				"%w: open workload network namespace for PID %d: %v",
				ErrImportWorkloadGone,
				pid,
				err,
			)
		}
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
			if errors.Is(err, unix.EADDRINUSE) {
				return nil, fmt.Errorf(
					"%w: bind workload localhost:%d in PID %d: %v",
					ErrImportPortConflict,
					port,
					pid,
					err,
				)
			}
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
