package loopback

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type Controller interface {
	Sync(context.Context) (Status, error)
	Status() Status
}

type controlRequest struct {
	Command string `json:"command"`
}

type controlResponse struct {
	Status *Status `json:"status,omitempty"`
	Error  string  `json:"error,omitempty"`
}

func ServeControl(ctx context.Context, path string, controller Controller) error {
	if controller == nil {
		return errors.New("loopback controller is required")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create loopback control directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect loopback control directory: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale loopback control socket: %w", err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return fmt.Errorf("listen on loopback control socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("protect loopback control socket: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	for {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			if ctx.Err() != nil || errors.Is(acceptErr, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("accept loopback control connection: %w", acceptErr)
		}
		go serveControlConnection(ctx, connection, controller)
	}
}

func Request(ctx context.Context, path string, command string) (Status, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return Status{}, fmt.Errorf("connect to loopback control socket: %w", err)
	}
	defer connection.Close()
	if deadline, exists := ctx.Deadline(); exists {
		if err := connection.SetDeadline(deadline); err != nil {
			return Status{}, fmt.Errorf("set loopback control deadline: %w", err)
		}
	}
	if err := json.NewEncoder(connection).Encode(controlRequest{Command: command}); err != nil {
		return Status{}, fmt.Errorf("write loopback control request: %w", err)
	}
	var response controlResponse
	if err := json.NewDecoder(connection).Decode(&response); err != nil {
		return Status{}, fmt.Errorf("read loopback control response: %w", err)
	}
	if response.Error != "" {
		return Status{}, errors.New(response.Error)
	}
	if response.Status == nil {
		return Status{}, errors.New("loopback control response has no status")
	}
	return cloneStatus(*response.Status), nil
}

func WriteStatusFile(path string, status Status) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create loopback status directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("protect loopback status directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".status-*")
	if err != nil {
		return fmt.Errorf("create loopback status file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set loopback status permissions: %w", err)
	}
	if err := json.NewEncoder(temporary).Encode(status); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("encode loopback status: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync loopback status: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close loopback status: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace loopback status: %w", err)
	}
	return nil
}

func serveControlConnection(ctx context.Context, connection net.Conn, controller Controller) {
	defer connection.Close()
	var request controlRequest
	if err := json.NewDecoder(connection).Decode(&request); err != nil {
		_ = json.NewEncoder(connection).Encode(controlResponse{Error: "decode control request: " + err.Error()})
		return
	}
	var status Status
	var err error
	switch request.Command {
	case "status":
		status = controller.Status()
	case "sync":
		status, err = controller.Sync(ctx)
	default:
		err = fmt.Errorf("unknown loopback command %q", request.Command)
	}
	response := controlResponse{Status: &status}
	if err != nil {
		response = controlResponse{Error: err.Error()}
	}
	_ = json.NewEncoder(connection).Encode(response)
}
