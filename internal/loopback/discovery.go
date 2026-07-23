package loopback

import (
	"fmt"
	"sort"
	"strings"
)

func Discover(containers []Container) Desired {
	sourcesByPort := make(map[uint16]map[string]struct{})
	warnings := make([]Warning, 0)

	for _, container := range containers {
		if !container.Running {
			continue
		}
		source := strings.TrimPrefix(container.Name, "/")
		if source == "" {
			source = container.ID
		}
		for _, binding := range container.Ports {
			if !binding.Published || binding.HostPort == 0 {
				continue
			}
			switch strings.ToLower(binding.Protocol) {
			case "tcp":
				sources := sourcesByPort[binding.HostPort]
				if sources == nil {
					sources = make(map[string]struct{})
					sourcesByPort[binding.HostPort] = sources
				}
				sources[source] = struct{}{}
			case "udp":
				warnings = append(warnings, Warning{
					Code:    "udp_unsupported",
					Port:    binding.HostPort,
					Source:  source,
					Message: fmt.Sprintf("UDP publication %s:%d is not proxied", source, binding.HostPort),
				})
			}
		}
	}

	ports := make([]int, 0, len(sourcesByPort))
	for port := range sourcesByPort {
		ports = append(ports, int(port))
	}
	sort.Ints(ports)

	publications := make([]Publication, 0, len(ports))
	for _, rawPort := range ports {
		port := uint16(rawPort)
		sources := make([]string, 0, len(sourcesByPort[port]))
		for source := range sourcesByPort[port] {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		publications = append(publications, Publication{
			Port:    port,
			Target:  fmt.Sprintf("docker:%d", port),
			Sources: sources,
		})
	}

	sort.Slice(warnings, func(left, right int) bool {
		if warnings[left].Port != warnings[right].Port {
			return warnings[left].Port < warnings[right].Port
		}
		return warnings[left].Source < warnings[right].Source
	})
	return Desired{Publications: publications, Warnings: warnings}
}
