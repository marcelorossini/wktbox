//go:build !windows

package executor

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func prepareCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func forwardSignals(process *os.Process) func() {
	signals := make(chan os.Signal, 4)
	done := make(chan struct{})
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for {
			select {
			case received := <-signals:
				if unixSignal, ok := received.(syscall.Signal); ok {
					_ = syscall.Kill(-process.Pid, unixSignal)
				}
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
