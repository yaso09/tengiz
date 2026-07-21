package types

import "testing"

func TestNixpacksConfigDefaults(t *testing.T) {
	cfg := BuildConfig{}
	if cfg.Builder != "" {
		t.Errorf("expected empty builder, got %q", cfg.Builder)
	}
	if cfg.NixpacksConfig != nil {
		t.Error("expected nil NixpacksConfig")
	}
}

func TestNixpacksConfigFields(t *testing.T) {
	cfg := BuildConfig{
		Builder: "nixpacks",
		NixpacksConfig: &NixpacksConfig{
			Packages: []string{"ffmpeg"},
		},
	}
	if cfg.Builder != "nixpacks" {
		t.Errorf("expected nixpacks, got %q", cfg.Builder)
	}
	if len(cfg.NixpacksConfig.Packages) != 1 || cfg.NixpacksConfig.Packages[0] != "ffmpeg" {
		t.Error("packages not set correctly")
	}
}
