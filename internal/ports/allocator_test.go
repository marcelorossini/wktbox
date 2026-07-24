package ports_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"wktbox/internal/ports"
)

func TestReserveReturnsFirstAvailableBlock(t *testing.T) {
	block, err := ports.NewAllocator(23000, 10).Reserve(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if block.Start != 23000 || block.Size != 10 {
		t.Fatalf("block = %#v", block)
	}
	if block.HTTP() != 23000 || block.HTTPS() != 23001 ||
		block.SSH() != 23002 || block.Gateway() != 23003 ||
		block.ImportRelay() != 23004 || block.BrowserCDP() != 23005 {
		t.Fatalf("service ports = %#v", block)
	}
}

func TestReserveSkipsOccupiedPortInCandidateBlock(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:23003")
	if err != nil {
		t.Skipf("port 23003 unavailable before test: %v", err)
	}
	defer listener.Close()

	block, err := ports.NewAllocator(23000, 10).Reserve(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if block.Start != 23010 {
		t.Fatalf("start = %d", block.Start)
	}
}

func TestReserveSkipsPersistedBlocks(t *testing.T) {
	used := []ports.Block{{Start: 23000, Size: 10}, {Start: 23010, Size: 10}}
	block, err := ports.NewAllocator(23000, 10).Reserve(context.Background(), used)
	if err != nil {
		t.Fatal(err)
	}
	if block.Start != 23020 {
		t.Fatalf("start = %d", block.Start)
	}
}

func TestReserveHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ports.NewAllocator(23000, 10).Reserve(ctx, nil)
	if err != context.Canceled {
		t.Fatalf("error = %v", err)
	}
}

func TestReserveRequiresRoomForBrowserCDP(t *testing.T) {
	_, err := ports.NewAllocator(23000, 5).Reserve(context.Background(), nil)
	if !errors.Is(err, ports.ErrNoPorts) {
		t.Fatalf("error = %v", err)
	}
}
