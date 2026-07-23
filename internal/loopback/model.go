package loopback

import "time"

const (
	EventStreamConnected    = "connected"
	EventStreamDisconnected = "disconnected"
	EventStreamUnavailable  = "unavailable"

	RouteListening = "listening"
	RouteConflict  = "conflict"
)

type PortBinding struct {
	ContainerPort uint16
	HostPort      uint16
	Protocol      string
	Published     bool
}

type Container struct {
	ID      string
	Name    string
	Running bool
	Ports   []PortBinding
}

type Publication struct {
	Port    uint16   `json:"port"`
	Target  string   `json:"target"`
	Sources []string `json:"sources"`
}

type Warning struct {
	Code    string `json:"code"`
	Port    uint16 `json:"port,omitempty"`
	Source  string `json:"source,omitempty"`
	Message string `json:"message"`
}

type Desired struct {
	Publications []Publication
	Warnings     []Warning
}

type Route struct {
	Port     uint16   `json:"port"`
	Target   string   `json:"target"`
	Sources  []string `json:"sources"`
	Protocol string   `json:"protocol"`
	State    string   `json:"state"`
	Error    string   `json:"error,omitempty"`
}

type Status struct {
	EventStream string    `json:"eventStream"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Routes      []Route   `json:"routes"`
	Warnings    []Warning `json:"warnings"`
}

func Unavailable(err error) Status {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return Status{
		EventStream: EventStreamUnavailable,
		UpdatedAt:   time.Now().UTC(),
		Warnings: []Warning{{
			Code:    "loopback_unavailable",
			Message: message,
		}},
	}
}
