package portforward

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type RelayOptions struct {
	Token           []byte
	Resolve         func(string) (string, bool)
	DialContext     func(context.Context, string, string) (net.Conn, error)
	PreambleTimeout time.Duration
}

type Relay struct {
	options   RelayOptions
	mutex     sync.Mutex
	listener  net.Listener
	closeOnce sync.Once
}

const (
	relayHealthMapping = "__wktbox_health__"
	relayStopMapping   = "__wktbox_stop__"
)

func NewRelay(options RelayOptions) *Relay {
	options.Token = append([]byte(nil), options.Token...)
	if options.DialContext == nil {
		dialer := &net.Dialer{}
		options.DialContext = dialer.DialContext
	}
	if options.PreambleTimeout <= 0 {
		options.PreambleTimeout = 5 * time.Second
	}
	return &Relay{options: options}
}

func (relay *Relay) Serve(ctx context.Context, listener net.Listener) error {
	if len(relay.options.Token) != TokenSize {
		return fmt.Errorf("relay token must contain exactly %d bytes", TokenSize)
	}
	if relay.options.Resolve == nil {
		return errors.New("relay target resolver is required")
	}
	relay.mutex.Lock()
	relay.listener = listener
	relay.mutex.Unlock()

	go func() {
		<-ctx.Done()
		_ = relay.Close()
	}()
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		go relay.handle(ctx, connection)
	}
}

func (relay *Relay) Close() error {
	var closeErr error
	relay.closeOnce.Do(func() {
		relay.mutex.Lock()
		defer relay.mutex.Unlock()
		if relay.listener != nil {
			closeErr = relay.listener.Close()
		}
	})
	return closeErr
}

func (relay *Relay) handle(ctx context.Context, source net.Conn) {
	defer source.Close()
	_ = source.SetDeadline(time.Now().Add(relay.options.PreambleTimeout))
	token, name, err := ReadPreamble(source)
	if err != nil {
		_ = writeRelayResponse(source, responseError, "invalid relay request")
		return
	}
	if subtle.ConstantTimeCompare(token, relay.options.Token) != 1 {
		_ = writeRelayResponse(source, responseError, "relay authentication failed")
		return
	}
	if name == relayHealthMapping {
		_ = writeRelayResponse(source, responseOK, "")
		return
	}
	if name == relayStopMapping {
		if writeRelayResponse(source, responseOK, "") == nil {
			_ = relay.Close()
		}
		return
	}
	targetAddress, exists := relay.options.Resolve(name)
	if !exists {
		_ = writeRelayResponse(source, responseError, "unknown relay mapping "+name)
		return
	}
	target, err := relay.options.DialContext(ctx, "tcp", targetAddress)
	if err != nil {
		_ = writeRelayResponse(source, responseError, "connect relay target: "+err.Error())
		return
	}
	defer target.Close()
	if err := writeRelayResponse(source, responseOK, ""); err != nil {
		return
	}
	_ = source.SetDeadline(time.Time{})
	copyBoth(source, target)
}

func ProbeRelay(ctx context.Context, address string, token []byte) error {
	connection, err := dialRelayCommand(ctx, address, token, relayHealthMapping)
	if err != nil {
		return err
	}
	return connection.Close()
}

func StopRelay(ctx context.Context, address string, token []byte) error {
	connection, err := dialRelayCommand(ctx, address, token, relayStopMapping)
	if err != nil {
		return err
	}
	return connection.Close()
}

func dialRelayCommand(
	ctx context.Context,
	address string,
	token []byte,
	command string,
) (net.Conn, error) {
	dialer := &net.Dialer{}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect host relay: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	}
	if err := WritePreamble(connection, token, command); err != nil {
		connection.Close()
		return nil, err
	}
	if err := readRelayResponse(connection); err != nil {
		connection.Close()
		return nil, err
	}
	_ = connection.SetDeadline(time.Time{})
	return connection, nil
}

func DialRelay(
	ctx context.Context,
	address string,
	token []byte,
	mappingName string,
) (net.Conn, error) {
	return dialRelayCommand(ctx, address, token, mappingName)
}

func copyBoth(left net.Conn, right net.Conn) {
	var wait sync.WaitGroup
	wait.Add(2)
	copyOne := func(destination net.Conn, source net.Conn) {
		defer wait.Done()
		_, _ = io.Copy(destination, source)
		if closer, ok := destination.(interface{ CloseWrite() error }); ok {
			_ = closer.CloseWrite()
		}
	}
	go copyOne(left, right)
	go copyOne(right, left)
	wait.Wait()
}
