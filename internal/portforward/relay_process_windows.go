//go:build windows

package portforward

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func launchRelayProcess(executable string, options RelayProcessOptions) error {
	logFile, err := os.OpenFile(
		options.LogPath,
		os.O_CREATE|os.O_APPEND|os.O_WRONLY,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("open relay log: %w", err)
	}
	command := exec.Command(
		executable,
		"__port-relay",
		"--listen", options.ListenAddress,
		"--config", options.ConfigPath,
		"--token", options.TokenPath,
		"--pid", options.PIDPath,
	)
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
	if err := command.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start host import relay: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		logFile.Close()
		return fmt.Errorf("release host import relay: %w", err)
	}
	return logFile.Close()
}
