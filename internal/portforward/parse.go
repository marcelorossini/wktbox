package portforward

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

func ParseImports(positional []string, mapped []string) ([]Mapping, error) {
	if len(positional) != 0 && len(mapped) != 0 {
		return nil, fmt.Errorf("use either positional ports or --map, not both")
	}
	if len(positional) == 0 && len(mapped) == 0 {
		return nil, fmt.Errorf("at least one host port or --map is required")
	}

	mappings := make([]Mapping, 0, len(positional)+len(mapped))
	for _, value := range positional {
		port, err := parsePort(value)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, Mapping{
			Name:          "import-" + strconv.Itoa(int(port)),
			Direction:     Import,
			SourceAddress: "127.0.0.1",
			SourcePort:    port,
			TargetPort:    port,
		})
	}
	for _, value := range mapped {
		mapping, err := parseMap(value, Import)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	if err := ValidateSet(mappings); err != nil {
		return nil, err
	}
	Sort(mappings)
	return mappings, nil
}

func ParsePublications(mapped []string) ([]Mapping, error) {
	if len(mapped) == 0 {
		return nil, fmt.Errorf("at least one --map is required")
	}
	mappings := make([]Mapping, 0, len(mapped))
	for _, value := range mapped {
		mapping, err := parseMap(value, Publish)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	if err := ValidateSet(mappings); err != nil {
		return nil, err
	}
	Sort(mappings)
	return mappings, nil
}

func parseMap(value string, direction Direction) (Mapping, error) {
	name, ports, found := strings.Cut(value, "=")
	if !found || name == "" || ports == "" {
		return Mapping{}, fmt.Errorf(
			"invalid mapping %q; expected name=address:source-port:target-port",
			value,
		)
	}
	targetSeparator := strings.LastIndexByte(ports, ':')
	if targetSeparator < 0 {
		return Mapping{}, fmt.Errorf(
			"invalid mapping %q; expected name=address:source-port:target-port",
			value,
		)
	}
	source := ports[:targetSeparator]
	targetText := ports[targetSeparator+1:]
	address, sourceText, err := net.SplitHostPort(source)
	if err != nil {
		return Mapping{}, fmt.Errorf("invalid mapping %q: %w", value, err)
	}
	sourcePort, err := parsePort(sourceText)
	if err != nil {
		return Mapping{}, fmt.Errorf("invalid mapping %q: %w", value, err)
	}
	targetPort, err := parsePort(targetText)
	if err != nil {
		return Mapping{}, fmt.Errorf("invalid mapping %q: %w", value, err)
	}
	mapping := Mapping{
		Name:          name,
		Direction:     direction,
		SourceAddress: address,
		SourcePort:    sourcePort,
		TargetPort:    targetPort,
	}
	if err := Validate(mapping); err != nil {
		return Mapping{}, err
	}
	return mapping, nil
}

func parsePort(value string) (uint16, error) {
	parsed, err := strconv.ParseUint(value, 10, 16)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid port %q; use a value from 1 to 65535", value)
	}
	return uint16(parsed), nil
}
