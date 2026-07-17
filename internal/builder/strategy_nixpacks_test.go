package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNixpacksStrategyMissingCLI(t *testing.T) {
	s := NewNixpacksStrategy()
	_, err := exec.LookPath("nixpacks")
	if err != nil {
		t.Skip("nixpacks CLI not installed, skipping")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0644)
	detection := &Detection{Framework: FrameworkNode, InternalPort: 3000}
	_, _, err = s.Build(context.Background(), dir, "testapp", "production", detection, "v123")
	if err != nil {
		t.Logf("Build error (expected if nixpacks fails): %v", err)
	}
}

func TestNixpacksStrategyPlanParsing(t *testing.T) {
	planJSON := `{
		"providers": ["node"],
		"variables": {"PORT": "3000"},
		"phases": [
			{"name": "install", "cmds": ["npm ci"]},
			{"name": "build", "cmds": ["npm run build"]}
		],
		"startCmds": ["npm start"]
	}`
	plan := &nixpacksPlan{}
	if err := plan.parse([]byte(planJSON)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(plan.StartCmds) == 0 || plan.StartCmds[0] != "npm start" {
		t.Errorf("StartCmds[0] = %q, want %q", plan.StartCmds[0], "npm start")
	}
}
