package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v4"

	"wktbox/assets"
	"wktbox/internal/gateway"
	"wktbox/internal/portforward"
)

var boxIDPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

type projectEnvOverride struct {
	Services map[string]overrideService `yaml:"services"`
}

type overrideService struct {
	Environment map[string]string `yaml:"environment,omitempty"`
	Volumes     []overrideVolume  `yaml:"volumes,omitempty"`
}

type overrideVolume struct {
	Type     string `yaml:"type"`
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"read_only"`
}

func Render(directory string, spec Spec) (Files, error) {
	if err := validateSpec(spec); err != nil {
		return Files{}, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return Files{}, fmt.Errorf("create box directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return Files{}, fmt.Errorf("protect box directory: %w", err)
	}

	files := Files{
		ComposePath:        filepath.Join(directory, "compose.yml"),
		SandboxEnvPath:     filepath.Join(directory, "sandbox.env"),
		GatewayConfigPath:  filepath.Join(directory, "gateway.conf"),
		PortConfigPath:     filepath.Join(directory, "ports", "ports.json"),
		PortOverridePath:   filepath.Join(directory, "ports.override.yml"),
		PortRelayTokenPath: filepath.Join(directory, "ports", "relay.token"),
		PortTransactionLockPath: filepath.Join(
			directory,
			"port-transaction",
			"transaction.lock",
		),
	}
	if err := os.MkdirAll(filepath.Dir(files.PortConfigPath), 0o700); err != nil {
		return Files{}, fmt.Errorf("create port runtime directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(files.PortConfigPath), 0o700); err != nil {
		return Files{}, fmt.Errorf("protect port runtime directory: %w", err)
	}
	if err := os.MkdirAll(
		filepath.Dir(files.PortTransactionLockPath),
		0o700,
	); err != nil {
		return Files{}, fmt.Errorf("create port transaction directory: %w", err)
	}
	if err := os.Chmod(
		filepath.Dir(files.PortTransactionLockPath),
		0o700,
	); err != nil {
		return Files{}, fmt.Errorf("protect port transaction directory: %w", err)
	}
	transactionLock, err := os.OpenFile(
		files.PortTransactionLockPath,
		os.O_CREATE|os.O_RDWR,
		0o600,
	)
	if err != nil {
		return Files{}, fmt.Errorf("create port transaction lock: %w", err)
	}
	if err := transactionLock.Close(); err != nil {
		return Files{}, fmt.Errorf("close port transaction lock: %w", err)
	}
	if err := os.Chmod(files.PortTransactionLockPath, 0o600); err != nil {
		return Files{}, fmt.Errorf("protect port transaction lock: %w", err)
	}
	_, tokenStatErr := os.Stat(files.PortRelayTokenPath)
	_, err = portforward.EnsureRelayToken(files.PortRelayTokenPath)
	if err != nil {
		return Files{}, err
	}
	if os.IsNotExist(tokenStatErr) {
		files.Changed = true
	} else if tokenStatErr != nil {
		return Files{}, fmt.Errorf("inspect port relay token: %w", tokenStatErr)
	}
	changed, err := writeProtected(files.ComposePath, assets.SandboxCompose)
	if err != nil {
		return Files{}, err
	}
	files.Changed = files.Changed || changed
	gatewayConfig, err := gateway.Render(spec.Config.Gateway.Routes, spec.ID)
	if err != nil {
		return Files{}, err
	}
	changed, err = writeProtected(files.GatewayConfigPath, gatewayConfig)
	if err != nil {
		return Files{}, err
	}
	files.Changed = files.Changed || changed
	changed, err = writeProtected(
		files.SandboxEnvPath,
		renderSandboxEnv(spec, files.GatewayConfigPath),
	)
	if err != nil {
		return Files{}, err
	}
	files.Changed = files.Changed || changed
	portConfig, err := portforward.RenderConfig(spec.PortMappings)
	if err != nil {
		return Files{}, err
	}
	changed, err = writeProtected(files.PortConfigPath, portConfig)
	if err != nil {
		return Files{}, err
	}
	files.Changed = files.Changed || changed
	portOverride, err := portforward.RenderComposeOverride(spec.PortMappings)
	if err != nil {
		return Files{}, err
	}
	if portOverride != nil {
		changed, err = writeProtected(files.PortOverridePath, portOverride)
		if err != nil {
			return Files{}, err
		}
		files.Changed = files.Changed || changed
	} else if err := os.Remove(files.PortOverridePath); err == nil {
		files.Changed = true
		files.PortOverridePath = ""
	} else if os.IsNotExist(err) {
		files.PortOverridePath = ""
	} else {
		return Files{}, fmt.Errorf("remove stale port override: %w", err)
	}

	overridePath := filepath.Join(directory, "project-env.override.yml")
	if spec.ProjectEnv.Mount ||
		len(spec.GitBridge.Mounts) != 0 ||
		len(spec.GitBridge.Environment) != 0 {
		override, err := renderRuntimeOverride(spec)
		if err != nil {
			return Files{}, err
		}
		changed, err = writeProtected(overridePath, override)
		if err != nil {
			return Files{}, err
		}
		files.Changed = files.Changed || changed
		files.ProjectEnvOverridePath = overridePath
	} else if err := os.Remove(overridePath); err == nil {
		files.Changed = true
	} else if !os.IsNotExist(err) {
		return Files{}, fmt.Errorf("remove stale project env override: %w", err)
	}

	return files, nil
}

func RenderPortFiles(
	configPath string,
	overridePath string,
	mappings []portforward.Mapping,
) (bool, error) {
	if configPath == "" || overridePath == "" {
		return false, errors.New("port config and override paths are required")
	}
	config, err := portforward.RenderConfig(mappings)
	if err != nil {
		return false, err
	}
	if _, err := writeProtected(configPath, config); err != nil {
		return false, err
	}
	override, err := portforward.RenderComposeOverride(mappings)
	if err != nil {
		return false, err
	}
	if override == nil {
		if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove stale port override: %w", err)
		}
		return false, nil
	}
	if _, err := writeProtected(overridePath, override); err != nil {
		return false, err
	}
	return true, nil
}

func validateSpec(spec Spec) error {
	if !boxIDPattern.MatchString(spec.ID) {
		return fmt.Errorf("invalid box ID %q", spec.ID)
	}
	if strings.TrimSpace(spec.Worktree) == "" {
		return errors.New("worktree path is required")
	}
	if spec.Ports.Size < 5 || spec.Ports.Start < 1 || spec.Ports.End() > 65535 {
		return fmt.Errorf("invalid port block %#v", spec.Ports)
	}
	if spec.Config.Runtime.DindImage == "" || spec.Config.Runtime.WebtopImage == "" {
		return errors.New("DinD and webtop images are required")
	}
	if spec.ProjectEnv.Mount {
		if spec.ProjectEnv.Source == "" || spec.ProjectEnv.Target == "" {
			return errors.New("mounted project env requires source and target")
		}
		if !spec.ProjectEnv.ReadOnly {
			return errors.New("project env mount must be read-only")
		}
	}
	return nil
}

func renderSandboxEnv(spec Spec, gatewayConfigPath string) []byte {
	values := map[string]string{
		"GATEWAY_CONFIG_PATH":  gatewayConfigPath,
		"PGID":                 strconv.Itoa(spec.PGID),
		"PORT_GATEWAY":         strconv.Itoa(spec.Ports.Gateway()),
		"PORT_HTTP":            strconv.Itoa(spec.Ports.HTTP()),
		"PORT_HTTPS":           strconv.Itoa(spec.Ports.HTTPS()),
		"PORT_IMPORT_RELAY":    strconv.Itoa(spec.Ports.ImportRelay()),
		"PORT_SSH":             strconv.Itoa(spec.Ports.SSH()),
		"PUID":                 strconv.Itoa(spec.PUID),
		"SHM_SIZE":             spec.Config.Webtop.SHMSize,
		"TZ":                   spec.Timezone,
		"WKTBOX_DIND_IMAGE":    spec.Config.Runtime.DindImage,
		"WKTBOX_GATEWAY_IMAGE": spec.Config.Runtime.GatewayImage,
		"WKTBOX_ID":            spec.ID,
		"WKTBOX_VERSION":       spec.Version,
		"WKTBOX_WEBTOP_IMAGE":  spec.Config.Runtime.WebtopImage,
		"WORKTREE_PATH":        spec.Worktree,
	}
	values["PORT_RUNTIME_PATH"] = filepath.Join(
		filepath.Dir(gatewayConfigPath),
		"ports",
	)
	values["PORT_TRANSACTION_PATH"] = filepath.Join(
		filepath.Dir(gatewayConfigPath),
		"port-transaction",
	)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var output strings.Builder
	for _, key := range keys {
		output.WriteString(key)
		output.WriteByte('=')
		output.WriteString(strconv.Quote(values[key]))
		output.WriteByte('\n')
	}
	return []byte(output.String())
}

func renderRuntimeOverride(spec Spec) ([]byte, error) {
	document := projectEnvOverride{
		Services: make(map[string]overrideService),
	}
	if spec.ProjectEnv.Mount {
		mount := overrideVolume{
			Type:     "bind",
			Source:   spec.ProjectEnv.Source,
			Target:   spec.ProjectEnv.Target,
			ReadOnly: true,
		}
		document.Services["docker"] = overrideService{Volumes: []overrideVolume{mount}}
		document.Services["webtop"] = overrideService{Volumes: []overrideVolume{mount}}
	}
	if len(spec.GitBridge.Mounts) != 0 || len(spec.GitBridge.Environment) != 0 {
		webtop := document.Services["webtop"]
		for _, mount := range spec.GitBridge.Mounts {
			webtop.Volumes = append(webtop.Volumes, overrideVolume{
				Type:     "bind",
				Source:   mount.Source,
				Target:   mount.Target,
				ReadOnly: mount.ReadOnly,
			})
		}
		if len(spec.GitBridge.Environment) != 0 {
			webtop.Environment = make(map[string]string, len(spec.GitBridge.Environment))
			for key, value := range spec.GitBridge.Environment {
				webtop.Environment[key] = value
			}
		}
		document.Services["webtop"] = webtop
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode project env override: %w", err)
	}
	return data, nil
}

func writeProtected(path string, data []byte) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		if err := os.Chmod(path, 0o600); err != nil {
			return false, fmt.Errorf("protect %s: %w", filepath.Base(path), err)
		}
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return false, fmt.Errorf("create temporary %s: %w", filepath.Base(path), err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return false, fmt.Errorf("protect temporary %s: %w", filepath.Base(path), err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false, fmt.Errorf("write temporary %s: %w", filepath.Base(path), err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return false, fmt.Errorf("sync temporary %s: %w", filepath.Base(path), err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close temporary %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return false, fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return true, nil
}
