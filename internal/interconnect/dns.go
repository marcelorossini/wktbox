package interconnect

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

const maximumDNSMessageSize = 65535

type DNSOptions struct {
	ListenAddress   string
	UpstreamAddress string
	Timeout         time.Duration
}

func LocalAddressForRoute(destination string, port int) (string, error) {
	if destination == "" {
		return "", errors.New("route destination is required")
	}
	if port < 1 || port > 65535 {
		return "", errors.New("route address port is invalid")
	}
	connection, err := net.Dial("udp", destination)
	if err != nil {
		return "", fmt.Errorf("resolve local route address: %w", err)
	}
	defer connection.Close()
	local, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil {
		return "", errors.New("resolve local route address: unexpected UDP address")
	}
	return net.JoinHostPort(local.IP.String(), strconv.Itoa(port)), nil
}

func ServeDNS(ctx context.Context, options DNSOptions) error {
	if options.ListenAddress == "" {
		return errors.New("DNS listen address is required")
	}
	if options.UpstreamAddress == "" {
		return errors.New("DNS upstream address is required")
	}
	if options.Timeout <= 0 {
		options.Timeout = 3 * time.Second
	}

	udpListener, err := net.ListenPacket("udp", options.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for DNS over UDP: %w", err)
	}
	defer udpListener.Close()
	tcpListener, err := net.Listen("tcp", options.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for DNS over TCP: %w", err)
	}
	defer tcpListener.Close()

	results := make(chan error, 2)
	serverDone := make(chan struct{})
	defer close(serverDone)
	go func() {
		results <- serveDNSUDP(ctx, udpListener, options)
	}()
	go func() {
		results <- serveDNSTCP(ctx, tcpListener, options)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = udpListener.Close()
			_ = tcpListener.Close()
		case <-serverDone:
		}
	}()

	first := <-results
	_ = udpListener.Close()
	_ = tcpListener.Close()
	second := <-results
	if ctx.Err() != nil {
		return nil
	}
	return errors.Join(normalizeListenerError(first), normalizeListenerError(second))
}

func serveDNSUDP(
	ctx context.Context,
	listener net.PacketConn,
	options DNSOptions,
) error {
	for {
		message := make([]byte, maximumDNSMessageSize)
		size, client, err := listener.ReadFrom(message)
		if err != nil {
			return err
		}
		message = message[:size]
		go forwardDNSUDP(ctx, listener, client, message, options)
	}
}

func forwardDNSUDP(
	ctx context.Context,
	listener net.PacketConn,
	client net.Addr,
	query []byte,
	options DNSOptions,
) {
	connection, err := (&net.Dialer{Timeout: options.Timeout}).DialContext(
		ctx,
		"udp",
		options.UpstreamAddress,
	)
	if err != nil {
		return
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(options.Timeout))
	if _, err := connection.Write(query); err != nil {
		return
	}
	response := make([]byte, maximumDNSMessageSize)
	size, err := connection.Read(response)
	if err != nil {
		return
	}
	_, _ = listener.WriteTo(response[:size], client)
}

func serveDNSTCP(
	ctx context.Context,
	listener net.Listener,
	options DNSOptions,
) error {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return err
		}
		go handleDNSTCP(ctx, connection, options)
	}
}

func handleDNSTCP(
	ctx context.Context,
	client net.Conn,
	options DNSOptions,
) {
	defer client.Close()
	for {
		_ = client.SetDeadline(time.Now().Add(options.Timeout))
		query, err := readDNSFrame(client)
		if err != nil {
			return
		}
		upstream, err := (&net.Dialer{Timeout: options.Timeout}).DialContext(
			ctx,
			"tcp",
			options.UpstreamAddress,
		)
		if err != nil {
			return
		}
		_ = upstream.SetDeadline(time.Now().Add(options.Timeout))
		if err := writeDNSFrame(upstream, query); err != nil {
			_ = upstream.Close()
			return
		}
		response, err := readDNSFrame(upstream)
		_ = upstream.Close()
		if err != nil {
			return
		}
		if err := writeDNSFrame(client, response); err != nil {
			return
		}
	}
}

func readDNSFrame(reader io.Reader) ([]byte, error) {
	var size uint16
	if err := binary.Read(reader, binary.BigEndian, &size); err != nil {
		return nil, err
	}
	message := make([]byte, size)
	if _, err := io.ReadFull(reader, message); err != nil {
		return nil, err
	}
	return message, nil
}

func writeDNSFrame(writer io.Writer, message []byte) error {
	if len(message) > maximumDNSMessageSize {
		return errors.New("DNS message exceeds TCP frame size")
	}
	if err := binary.Write(writer, binary.BigEndian, uint16(len(message))); err != nil {
		return err
	}
	_, err := writer.Write(message)
	return err
}

func ProbeDNS(ctx context.Context, server string) error {
	if server == "" {
		return errors.New("DNS server address is required")
	}
	query, transactionID, err := probeQuery()
	if err != nil {
		return err
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "udp", server)
	if err != nil {
		return fmt.Errorf("connect to DNS server: %w", err)
	}
	defer connection.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
	} else {
		_ = connection.SetDeadline(time.Now().Add(3 * time.Second))
	}
	if _, err := connection.Write(query); err != nil {
		return fmt.Errorf("send DNS probe: %w", err)
	}
	response := make([]byte, maximumDNSMessageSize)
	size, err := connection.Read(response)
	if err != nil {
		return fmt.Errorf("receive DNS probe: %w", err)
	}
	if size < 12 {
		return errors.New("DNS probe returned a truncated response")
	}
	if binary.BigEndian.Uint16(response[:2]) != transactionID {
		return errors.New("DNS probe returned a mismatched transaction ID")
	}
	if response[2]&0x80 == 0 {
		return errors.New("DNS probe did not return a response")
	}
	return nil
}

func probeQuery() ([]byte, uint16, error) {
	var identifier [2]byte
	if _, err := rand.Read(identifier[:]); err != nil {
		return nil, 0, fmt.Errorf("generate DNS transaction ID: %w", err)
	}
	transactionID := binary.BigEndian.Uint16(identifier[:])
	query := []byte{
		identifier[0], identifier[1],
		0x01, 0x00,
		0x00, 0x01,
		0x00, 0x00,
		0x00, 0x00,
		0x00, 0x00,
		0x09, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't',
		0x00,
		0x00, 0x01,
		0x00, 0x01,
	}
	return query, transactionID, nil
}

func normalizeListenerError(err error) error {
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
