package portforward

import (
	"encoding/json"
	"fmt"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	Mappings []Mapping `json:"mappings"`
}

type composeOverride struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Ports []string `yaml:"ports"`
}

func RenderConfig(mappings []Mapping) ([]byte, error) {
	sorted := append([]Mapping(nil), mappings...)
	Sort(sorted)
	if err := ValidateSet(sorted); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(Config{Mappings: sorted}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode port configuration: %w", err)
	}
	return append(body, '\n'), nil
}

func RenderComposeOverride(mappings []Mapping) ([]byte, error) {
	if err := ValidateSet(mappings); err != nil {
		return nil, err
	}
	publications := Filter(mappings, Publish)
	if len(publications) == 0 {
		return nil, nil
	}
	ports := make([]string, 0, len(publications))
	for _, mapping := range publications {
		port := fmt.Sprintf(
			"%s:%d:%d",
			mapping.SourceAddress,
			mapping.SourcePort,
			mapping.SourcePort,
		)
		ports = append(ports, port)
	}
	body, err := yaml.Marshal(composeOverride{
		Services: map[string]composeService{
			"portbridge": {Ports: ports},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("encode port Compose override: %w", err)
	}
	return body, nil
}
