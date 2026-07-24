package ports

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
)

var ErrNoPorts = errors.New("no complete port block is available")

type Block struct {
	Start int `json:"start"`
	Size  int `json:"size"`
}

func (block Block) HTTP() int {
	return block.Start
}

func (block Block) HTTPS() int {
	return block.Start + 1
}

func (block Block) SSH() int {
	return block.Start + 2
}

func (block Block) Gateway() int {
	return block.Start + 3
}

func (block Block) ImportRelay() int {
	return block.Start + 4
}

func (block Block) End() int {
	return block.Start + block.Size - 1
}

type Allocator struct {
	base      int
	blockSize int
}

func NewAllocator(base int, blockSize int) Allocator {
	return Allocator{base: base, blockSize: blockSize}
}

func (allocator Allocator) Reserve(ctx context.Context, used []Block) (Block, error) {
	if err := ctx.Err(); err != nil {
		return Block{}, err
	}
	if allocator.base < 1 || allocator.blockSize < 5 {
		return Block{}, fmt.Errorf(
			"%w: invalid range base=%d size=%d",
			ErrNoPorts,
			allocator.base,
			allocator.blockSize,
		)
	}

	for start := allocator.base; start+allocator.blockSize-1 <= 65535; start += allocator.blockSize {
		if err := ctx.Err(); err != nil {
			return Block{}, err
		}
		candidate := Block{Start: start, Size: allocator.blockSize}
		if overlapsAny(candidate, used) {
			continue
		}
		if blockAvailable(ctx, candidate) {
			return candidate, nil
		}
	}
	return Block{}, ErrNoPorts
}

func overlapsAny(candidate Block, used []Block) bool {
	for _, block := range used {
		if candidate.Start <= block.End() && block.Start <= candidate.End() {
			return true
		}
	}
	return false
}

func blockAvailable(ctx context.Context, block Block) bool {
	listeners := make([]net.Listener, 0, block.Size)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	var listenConfig net.ListenConfig
	for port := block.Start; port <= block.End(); port++ {
		host := "127.0.0.1"
		if port == block.ImportRelay() {
			host = "0.0.0.0"
		}
		listener, err := listenConfig.Listen(
			ctx,
			"tcp4",
			net.JoinHostPort(host, strconv.Itoa(port)),
		)
		if err != nil {
			return false
		}
		listeners = append(listeners, listener)
	}
	return true
}
