package portforward

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"time"
)

type Direction string

const (
	Import  Direction = "import"
	Publish Direction = "publish"
)

const (
	StateReady    = "ready"
	StateDegraded = "degraded"
	StateStopped  = "stopped"
)

var mappingNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

type Mapping struct {
	Name          string    `json:"name"`
	Direction     Direction `json:"direction"`
	SourceAddress string    `json:"sourceAddress"`
	SourcePort    uint16    `json:"sourcePort"`
	TargetPort    uint16    `json:"targetPort"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ObservedMapping struct {
	Mapping
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

func ValidateSet(mappings []Mapping) error {
	names := make(map[string]struct{}, len(mappings))
	publicationListeners := make(map[string]string)
	importTargets := make(map[uint16]string)
	for _, mapping := range mappings {
		if err := Validate(mapping); err != nil {
			return err
		}
		if _, exists := names[mapping.Name]; exists {
			return fmt.Errorf("duplicate mapping name %q", mapping.Name)
		}
		names[mapping.Name] = struct{}{}
		switch mapping.Direction {
		case Import:
			if previous, exists := importTargets[mapping.TargetPort]; exists {
				return fmt.Errorf(
					"duplicate import target localhost:%d in mappings %q and %q",
					mapping.TargetPort,
					previous,
					mapping.Name,
				)
			}
			importTargets[mapping.TargetPort] = mapping.Name
		case Publish:
			listener := net.JoinHostPort(
				mapping.SourceAddress,
				strconv.Itoa(int(mapping.SourcePort)),
			)
			if previous, exists := publicationListeners[listener]; exists {
				return fmt.Errorf(
					"duplicate publication listener %s in mappings %q and %q",
					listener,
					previous,
					mapping.Name,
				)
			}
			publicationListeners[listener] = mapping.Name
		}
	}
	return nil
}

func Validate(mapping Mapping) error {
	if !mappingNamePattern.MatchString(mapping.Name) {
		return fmt.Errorf(
			"invalid mapping name %q; use lowercase letters, digits, dot, underscore, or dash",
			mapping.Name,
		)
	}
	if mapping.Direction != Import && mapping.Direction != Publish {
		return fmt.Errorf(
			"mapping %q has invalid direction %q",
			mapping.Name,
			mapping.Direction,
		)
	}
	address := net.ParseIP(mapping.SourceAddress)
	if address == nil || !address.IsLoopback() {
		return fmt.Errorf(
			"mapping %q source address %q must be loopback",
			mapping.Name,
			mapping.SourceAddress,
		)
	}
	if mapping.Direction == Publish &&
		mapping.SourceAddress != "127.0.0.1" &&
		mapping.SourceAddress != "::1" {
		return fmt.Errorf(
			"mapping %q publication address %q must be 127.0.0.1 or ::1",
			mapping.Name,
			mapping.SourceAddress,
		)
	}
	if mapping.SourcePort == 0 {
		return fmt.Errorf("mapping %q has invalid source port 0", mapping.Name)
	}
	if mapping.TargetPort == 0 {
		return fmt.Errorf("mapping %q has invalid target port 0", mapping.Name)
	}
	return nil
}

func Sort(mappings []Mapping) {
	sort.Slice(mappings, func(left int, right int) bool {
		if mappings[left].Direction != mappings[right].Direction {
			return mappings[left].Direction < mappings[right].Direction
		}
		return mappings[left].Name < mappings[right].Name
	})
}

func Filter(mappings []Mapping, direction Direction) []Mapping {
	filtered := make([]Mapping, 0, len(mappings))
	for _, mapping := range mappings {
		if mapping.Direction == direction {
			filtered = append(filtered, mapping)
		}
	}
	Sort(filtered)
	return filtered
}
