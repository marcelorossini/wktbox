package config

type GitMode string

const (
	GitHost    GitMode = "host"
	GitMounted GitMode = "mounted"
)

type Config struct {
	Version     int                `yaml:"version"`
	Workspace   Workspace          `yaml:"workspace"`
	Environment Environment        `yaml:"environment"`
	Runtime     Runtime            `yaml:"runtime"`
	Webtop      Webtop             `yaml:"webtop"`
	Git         Git                `yaml:"git"`
	Resources   Resources          `yaml:"resources"`
	Gateway     Gateway            `yaml:"gateway"`
	Commands    map[string]Command `yaml:"commands"`
	Lifecycle   Lifecycle          `yaml:"lifecycle"`
}

type Workspace struct {
	Target string `yaml:"target"`
}

type Environment struct {
	File     string `yaml:"file"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"readOnly"`
}

type Runtime struct {
	DindImage    string `yaml:"dindImage"`
	WebtopImage  string `yaml:"webtopImage"`
	GatewayImage string `yaml:"gatewayImage"`
}

type Webtop struct {
	Enabled bool   `yaml:"enabled"`
	SHMSize string `yaml:"shmSize"`
	SSH     SSH    `yaml:"ssh"`
}

type SSH struct {
	Enabled bool `yaml:"enabled"`
}

type Git struct {
	Mode GitMode `yaml:"mode"`
}

type Resources struct {
	CPUs   float64 `yaml:"cpus"`
	Memory string  `yaml:"memory"`
	PIDs   int     `yaml:"pids"`
}

type Gateway struct {
	Enabled bool             `yaml:"enabled"`
	Routes  map[string]Route `yaml:"routes"`
}

type Route struct {
	Port int `yaml:"port"`
}

type Command struct {
	Command []string `yaml:"command"`
}

type Lifecycle struct {
	IdleTimeout            int  `yaml:"idleTimeout"`
	RemoveOnWorktreeDelete bool `yaml:"removeOnWorktreeDelete"`
}

type Overrides struct {
	ConfigPath string
	EnvFile    string
	EnvTarget  string
}

func Default() Config {
	return Config{
		Version: 1,
		Workspace: Workspace{
			Target: "/workspace",
		},
		Environment: Environment{
			Target:   "/workspace/.env",
			ReadOnly: true,
		},
		Runtime: Runtime{
			DindImage:    "docker:29.5.0-dind",
			WebtopImage:  "wktbox/webtop:dev",
			GatewayImage: "wktbox/gateway:dev",
		},
		Webtop: Webtop{
			Enabled: true,
			SHMSize: "1gb",
		},
		Git: Git{
			Mode: GitHost,
		},
		Resources: Resources{
			PIDs: 2048,
		},
		Gateway: Gateway{
			Routes: make(map[string]Route),
		},
		Commands: make(map[string]Command),
	}
}
