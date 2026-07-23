package sandbox

import (
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
)

var boxIDPattern = regexp.MustCompile(`^[a-f0-9]{12}$`)

type projectEnvOverride struct {
	Services map[string]overrideService `yaml:"services"`
}

type overrideService struct {
	Volumes []overrideVolume `yaml:"volumes"`
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
		ComposePath:    filepath.Join(directory, "compose.yml"),
		SandboxEnvPath: filepath.Join(directory, "sandbox.env"),
	}
	if err := writeProtected(files.ComposePath, assets.SandboxCompose); err != nil {
		return Files{}, err
	}
	if err := writeProtected(files.SandboxEnvPath, renderSandboxEnv(spec)); err != nil {
		return Files{}, err
	}

	overridePath := filepath.Join(directory, "project-env.override.yml")
	if spec.ProjectEnv.Mount {
		override, err := renderProjectEnvOverride(spec)
		if err != nil {
			return Files{}, err
		}
		if err := writeProtected(overridePath, override); err != nil {
			return Files{}, err
		}
		files.ProjectEnvOverridePath = overridePath
	} else if err := os.Remove(overridePath); err != nil && !os.IsNotExist(err) {
		return Files{}, fmt.Errorf("remove stale project env override: %w", err)
	}

	return files, nil
}

func validateSpec(spec Spec) error {
	if !boxIDPattern.MatchString(spec.ID) {
		return fmt.Errorf("invalid box ID %q", spec.ID)
	}
	if strings.TrimSpace(spec.Worktree) == "" {
		return errors.New("worktree path is required")
	}
	if spec.Ports.Size < 4 || spec.Ports.Start < 1 || spec.Ports.End() > 65535 {
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

func renderSandboxEnv(spec Spec) []byte {
	values := map[string]string{
		"PGID":                 strconv.Itoa(spec.PGID),
		"PORT_GATEWAY":         strconv.Itoa(spec.Ports.Gateway()),
		"PORT_HTTP":            strconv.Itoa(spec.Ports.HTTP()),
		"PORT_HTTPS":           strconv.Itoa(spec.Ports.HTTPS()),
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

func renderProjectEnvOverride(spec Spec) ([]byte, error) {
	mount := overrideVolume{
		Type:     "bind",
		Source:   spec.ProjectEnv.Source,
		Target:   spec.ProjectEnv.Target,
		ReadOnly: true,
	}
	document := projectEnvOverride{
		Services: map[string]overrideService{
			"docker": {Volumes: []overrideVolume{mount}},
			"webtop": {Volumes: []overrideVolume{mount}},
		},
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("encode project env override: %w", err)
	}
	return data, nil
}

func writeProtected(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", filepath.Base(path), err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary %s: %w", filepath.Base(path), err)
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary %s: %w", filepath.Base(path), err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary %s: %w", filepath.Base(path), err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	return nil
}
