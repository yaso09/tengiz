package builder

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yaso09/tengiz/internal/types"
)

type Framework string

const (
	FrameworkNextJS Framework = "nextjs"
	FrameworkVite   Framework = "vite"
	FrameworkGo     Framework = "go"
	FrameworkNode   Framework = "node"
	FrameworkPython Framework = "python"
	FrameworkStatic Framework = "static"
	FrameworkDocker Framework = "docker"
)

type Detection struct {
	Framework    Framework
	BuildCmd     string
	OutputDir    string
	InternalPort int
	HealthCheck  *types.HealthCheckConfig
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

func DetectWithBuilder(dir string, builderType string) (*Detection, error) {
	if hasFile(dir, "Dockerfile") {
		return &Detection{Framework: FrameworkDocker, InternalPort: 8080}, nil
	}
	if builderType == "nixpacks" {
		return nixpacksDetect(dir)
	}
	return Detect(dir)
}

type nixpacksPlanJSON struct {
	Providers []string         `json:"providers"`
	Variables map[string]any   `json:"variables,omitempty"`
	Phases    []nixpacksPhase  `json:"phases,omitempty"`
	StartCmds []string         `json:"startCmds"`
}

func nixpacksDetect(dir string) (*Detection, error) {
	cmd := exec.Command("nixpacks", "plan", dir, "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nixpacks plan: %w", err)
	}

	var plan nixpacksPlanJSON
	if err := json.Unmarshal(output, &plan); err != nil {
		return nil, fmt.Errorf("parse nixpacks plan: %w", err)
	}

	detection := &Detection{
		InternalPort: 8080,
	}

	if portStr, ok := plan.Variables["PORT"]; ok {
		if port, err := strconv.Atoi(fmt.Sprintf("%v", portStr)); err == nil {
			detection.InternalPort = port
		}
	}

	if len(plan.Providers) > 0 {
		detection.Framework = Framework(plan.Providers[0])
	} else {
		detection.Framework = FrameworkNode
	}

	for _, phase := range plan.Phases {
		if phase.Name == "build" && len(phase.Cmds) > 0 {
			detection.BuildCmd = strings.Join(phase.Cmds, " && ")
		}
	}

	return detection, nil
}
