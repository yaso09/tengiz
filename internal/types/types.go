package types

import "time"

type GitConfig struct {
	Repo     string `mapstructure:"repo" json:"repo,omitempty"`
	Branch   string `mapstructure:"branch" json:"branch,omitempty"`
	Provider string `mapstructure:"provider" json:"provider,omitempty"`
}

type VolumeConfig struct {
	HostPath      string `mapstructure:"host_path" yaml:"host_path" json:"host_path"`
	ContainerPath string `mapstructure:"container_path" yaml:"container_path" json:"container_path"`
	ReadOnly      bool   `mapstructure:"read_only" yaml:"read_only" json:"read_only,omitempty"`
}

type WebhookConfig struct {
	Secret          string   `mapstructure:"secret,omitempty"`
	AllowedBranches []string `mapstructure:"allowed_branches,omitempty"`
	Port            int      `mapstructure:"port,omitempty"`
}

type AppConfig struct {
	Name        string              `mapstructure:"name"`
	Port        int                 `mapstructure:"port"`
	Build       BuildConfig         `mapstructure:"build"`
	Serverless  ServerlessConfig    `mapstructure:"serverless"`
	Domains     []string            `mapstructure:"domains"`
	HealthCheck *HealthCheckConfig  `mapstructure:"healthcheck,omitempty"`
	Resources   *ResourceConfig     `mapstructure:"resources,omitempty" json:"resources,omitempty"`
	Env         map[string]string   `mapstructure:"env" json:"env,omitempty"`
	Environment string              `mapstructure:"environment" json:"environment,omitempty"`
	Git         *GitConfig          `mapstructure:"git,omitempty" json:"git,omitempty"`
	Volumes     []VolumeConfig      `mapstructure:"volumes,omitempty" yaml:"volumes,omitempty" json:"volumes,omitempty"`
}

type ResourceConfig struct {
	CPU    string `mapstructure:"cpu" yaml:"cpu" json:"cpu,omitempty"`
	Memory string `mapstructure:"memory" yaml:"memory" json:"memory,omitempty"`
}

type BuildConfig struct {
	Command  string `mapstructure:"command"`
	Output   string `mapstructure:"output"`
	Strategy string `mapstructure:"strategy"`
}

type ServerlessConfig struct {
	Enabled     bool          `mapstructure:"enabled"`
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`
}

type AppState string

const (
	StateRunning  AppState = "running"
	StateStopped  AppState = "stopped"
	StateStarting AppState = "starting"
)

type AppStatus struct {
	Name      string    `json:"name"`
	State     AppState  `json:"state"`
	Port      int       `json:"port"`
	ImageHash string    `json:"image_hash"`
	CreatedAt time.Time `json:"created_at"`
	Domains   []string  `json:"domains"`
}

type PortEntry struct {
	AppName string `json:"app_name"`
	Port    int    `json:"port"`
}

type HealthCheckConfig struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled"`
	Endpoint    string `mapstructure:"endpoint" yaml:"endpoint"`
	Port        int    `mapstructure:"port" yaml:"port"`
	Interval    int    `mapstructure:"interval" yaml:"interval"`
	Retries     int    `mapstructure:"retries" yaml:"retries"`
	Timeout     int    `mapstructure:"timeout" yaml:"timeout"`
	StartPeriod int    `mapstructure:"start_period" yaml:"start_period"`
}

type DeploymentEntry struct {
	ID        string    `json:"id"`
	ImageTag  string    `json:"image_tag"`
	Port      int       `json:"port"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
}

type DeploymentStatus string

const (
	DeployActive   DeploymentStatus = "active"
	DeployPrevious DeploymentStatus = "previous"
	DeployRolled   DeploymentStatus = "rolled"
)

const (
	HealthUnknown   = "unknown"
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
)

type AppEntry struct {
	Name             string            `json:"name"`
	ImageTag         string            `json:"image_tag"`
	Port             int               `json:"port"`
	Domains          []string          `json:"domains"`
	Config           AppConfig         `json:"config"`
	Environment      string            `json:"environment,omitempty"`
	DeploymentSuffix string            `json:"deployment_suffix,omitempty"`
	Deployments      []DeploymentEntry `json:"deployments,omitempty"`
	RestartCount     int               `json:"restart_count,omitempty"`
	HealthStatus     string            `json:"health_status,omitempty"`
	GitRepo          string            `json:"git_repo,omitempty"`
	GitBranch        string            `json:"git_branch,omitempty"`
	GitProvider      string            `json:"git_provider,omitempty"`
}
