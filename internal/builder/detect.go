package builder

import (
	"os"
	"path/filepath"

	"github.com/yaso09/tengiz/internal/types"
)

type Framework string

const (
	FrameworkNextJS   Framework = "nextjs"
	FrameworkVite     Framework = "vite"
	FrameworkGo       Framework = "go"
	FrameworkNode     Framework = "node"
	FrameworkPython   Framework = "python"
	FrameworkStatic   Framework = "static"
	FrameworkDocker   Framework = "docker"
	FrameworkNixpacks Framework = "nixpacks"
)

type Detection struct {
	Framework    Framework
	BuildCmd     string
	OutputDir    string
	InternalPort int
	HealthCheck  *types.HealthCheckConfig
	Builder      string
}

func Detect(dir string) (*Detection, error) {
	if hasFile(dir, "Dockerfile") {
		return &Detection{Framework: FrameworkDocker, InternalPort: 8080}, nil
	}
	if hasFile(dir, "next.config.js") || hasFile(dir, "next.config.ts") || hasFile(dir, "next.config.mjs") {
		return &Detection{
			Framework:    FrameworkNextJS,
			BuildCmd:     "npm run build",
			OutputDir:    ".next",
			InternalPort: 3000,
		}, nil
	}
	if hasFile(dir, "vite.config.js") || hasFile(dir, "vite.config.ts") {
		return &Detection{
			Framework:    FrameworkVite,
			BuildCmd:     "npm run build",
			OutputDir:    "dist",
			InternalPort: 80,
		}, nil
	}
	if hasFile(dir, "go.mod") {
		return &Detection{
			Framework:    FrameworkGo,
			BuildCmd:     "go build -o app .",
			InternalPort: 8080,
		}, nil
	}
	if hasFile(dir, "package.json") {
		return &Detection{
			Framework:    FrameworkNode,
			BuildCmd:     "npm run build",
			InternalPort: 3000,
		}, nil
	}
	if hasFile(dir, "requirements.txt") || hasFile(dir, "Pipfile") || hasFile(dir, "pyproject.toml") {
		return &Detection{
			Framework:    FrameworkPython,
			InternalPort: 8000,
		}, nil
	}
	if hasFile(dir, "index.html") {
		return &Detection{
			Framework:    FrameworkStatic,
			OutputDir:    ".",
			InternalPort: 80,
		}, nil
	}
	return &Detection{Framework: FrameworkStatic, InternalPort: 80}, nil
}

func hasFile(dir, name string) bool {
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && !info.IsDir()
}
