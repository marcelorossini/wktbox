//go:build windows

package executor

import (
	"os"
	"os/exec"
	"os/signal"
)

func prepareCommand(*exec.Cmd) {}

func forwardSignals(process *os.Process) func() {
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt)
	go func() {
		for {
			select {
			case received := <-signals:
				_ = process.Signal(received)
			case <-done:
				return
			}
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}
