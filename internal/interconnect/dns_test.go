package interconnect_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"wktbox/internal/interconnect"
)

func TestServeDNSForwardsUDPResponse(t *testing.T) {
	upstream := startDNSFixture(t)
	listen := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 1)
	go func() {
		results <- interconnect.ServeDNS(ctx, interconnect.DNSOptions{
			ListenAddress:   listen,
			UpstreamAddress: upstream,
			Timeout:         time.Second,
		})
	}()

	query := testQuery()
	response := exchangeUDP(t, listen, query)
	if !bytes.Equal(response[:2], query[:2]) || response[2]&0x80 == 0 {
		t.Fatalf("response = %x", response)
	}
}

func TestServeDNSForwardsTCPResponse(t *testing.T) {
	upstream := startDNSFixture(t)
	listen := freeAddress(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 1)
	go func() {
		results <- interconnect.ServeDNS(ctx, interconnect.DNSOptions{
			ListenAddress:   listen,
			UpstreamAddress: upstream,
			Timeout:         time.Second,
		})
	}()

	query := testQuery()
	response := exchangeTCP(t, listen, query)
	if !bytes.Equal(response[:2], query[:2]) || response[2]&0x80 == 0 {
		t.Fatalf("response = %x", response)
	}
}

func TestProbeDNSValidatesTransactionResponse(t *testing.T) {
	upstream := startDNSFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := interconnect.ProbeDNS(ctx, upstream); err != nil {
		t.Fatal(err)
	}
}

func TestLocalAddressForRouteUsesSelectedInterfaceAndPort(t *testing.T) {
	got, err := interconnect.LocalAddressForRoute("127.0.0.1:9", 53)
	if err != nil {
		t.Fatal(err)
	}
	if got != "127.0.0.1:53" {
		t.Fatalf("address = %q", got)
	}
}

func TestServeDNSStopsAfterCancellation(t *testing.T) {
	upstream := startDNSFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan error, 1)
	go func() {
		results <- interconnect.ServeDNS(ctx, interconnect.DNSOptions{
			ListenAddress:   freeAddress(t),
			UpstreamAddress: upstream,
			Timeout:         time.Second,
		})
	}()
	cancel()

	select {
	case err := <-results:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("DNS server did not stop after cancellation")
	}
}

func startDNSFixture(t *testing.T) string {
	t.Helper()
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := tcpListener.Addr().String()
	udpAddress, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	udpListener, err := net.ListenUDP("udp", udpAddress)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = udpListener.Close()
		_ = tcpListener.Close()
	})

	go func() {
		buffer := make([]byte, 65535)
		for {
			size, client, err := udpListener.ReadFromUDP(buffer)
			if err != nil {
				return
			}
			response := append([]byte(nil), buffer[:size]...)
			if len(response) >= 3 {
				response[2] |= 0x80
			}
			_, _ = udpListener.WriteToUDP(response, client)
		}
	}()
	go func() {
		for {
			connection, err := tcpListener.Accept()
			if err != nil {
				return
			}
			go serveDNSFixtureTCP(connection)
		}
	}()
	return address
}

func serveDNSFixtureTCP(connection net.Conn) {
	defer connection.Close()
	var length uint16
	if err := binary.Read(connection, binary.BigEndian, &length); err != nil {
		return
	}
	query := make([]byte, length)
	if _, err := io.ReadFull(connection, query); err != nil {
		return
	}
	if len(query) >= 3 {
		query[2] |= 0x80
	}
	_ = binary.Write(connection, binary.BigEndian, uint16(len(query)))
	_, _ = connection.Write(query)
}

func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func exchangeUDP(t *testing.T, address string, query []byte) []byte {
	t.Helper()
	var lastErr error
	for attempt := 0; attempt < 50; attempt++ {
		connection, err := net.DialTimeout("udp", address, 50*time.Millisecond)
		if err != nil {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		_ = connection.SetDeadline(time.Now().Add(50 * time.Millisecond))
		if _, err := connection.Write(query); err != nil {
			lastErr = err
			_ = connection.Close()
			time.Sleep(10 * time.Millisecond)
			continue
		}
		response := make([]byte, 65535)
		size, err := connection.Read(response)
		_ = connection.Close()
		if err == nil {
			return response[:size]
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(lastErr)
	return nil
}

func exchangeTCP(t *testing.T, address string, query []byte) []byte {
	t.Helper()
	var connection net.Conn
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		connection, err = net.DialTimeout("tcp", address, 50*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if err := binary.Write(connection, binary.BigEndian, uint16(len(query))); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Write(query); err != nil {
		t.Fatal(err)
	}
	var length uint16
	if err := binary.Read(connection, binary.BigEndian, &length); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, length)
	if _, err := io.ReadFull(connection, response); err != nil {
		t.Fatal(err)
	}
	return response
}

func testQuery() []byte {
	return []byte{
		0x12, 0x34,
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
}
