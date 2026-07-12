package types

import "time"

type AppConfig struct {
	Name      string          `mapstructure:"name"`
	Port      int             `mapstructure:"port"`
	Build     BuildConfig     `mapstructure:"build"`
	Serverless ServerlessConfig `mapstructure:"serverless"`
	Domains   []string        `mapstructure:"domains"`
}

type BuildConfig struct {
	Command string `mapstructure:"command"`
	Output  string `mapstructure:"output"`
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

type AppEntry struct {
	Name     string   `json:"name"`
	ImageTag string   `json:"image_tag"`
	Port     int      `json:"port"`
	Domains  []string `json:"domains"`
	Config   AppConfig `json:"config"`
}
