package secrets

import (
	"os"
	"testing"
)

func TestDopplerProviderImplementsProvider(t *testing.T) {
	var _ Provider = (*DopplerProvider)(nil)
}

func TestDopplerProviderRequiresToken(t *testing.T) {
	_, err := NewDopplerProvider(DopplerConfig{})
	if err == nil {
		t.Fatal("expected error with empty config")
	}

	_, err = NewDopplerProvider(DopplerConfig{Token: "valid", Project: "", Config: "prod"})
	if err == nil {
		t.Fatal("expected error with empty project")
	}

	_, err = NewDopplerProvider(DopplerConfig{Token: "valid", Project: "myapp", Config: ""})
	if err == nil {
		t.Fatal("expected error with empty config")
	}
}

func TestDopplerProviderName(t *testing.T) {
	p, err := NewDopplerProvider(DopplerConfig{Token: "t", Project: "p", Config: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "doppler" {
		t.Fatalf("expected 'doppler', got %q", p.Name())
	}
}

func TestDopplerProviderSetGetUnsetList(t *testing.T) {
	token := os.Getenv("TENGIZ_DOPPLER_TOKEN")
	project := os.Getenv("TENGIZ_DOPPLER_PROJECT")
	config := os.Getenv("TENGIZ_DOPPLER_CONFIG")
	if token == "" || project == "" || config == "" {
		t.Skip("TENGIZ_DOPPLER_TOKEN, TENGIZ_DOPPLER_PROJECT, TENGIZ_DOPPLER_CONFIG not set")
	}
	p, err := NewDopplerProvider(DopplerConfig{
		Token:   token,
		Project: project,
		Config:  config,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := p.Set("testapp", "DOPPLER_KEY", "dplr_val"); err != nil {
		t.Fatal(err)
	}
	defer p.Unset("testapp", "DOPPLER_KEY")

	val, ok, err := p.Get("testapp", "DOPPLER_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || val != "dplr_val" {
		t.Fatalf("expected dplr_val, got %q (ok=%v)", val, ok)
	}

	secrets, err := p.List("testapp")
	if err != nil {
		t.Fatal(err)
	}
	if secrets["DOPPLER_KEY"] != "dplr_val" {
		t.Fatal("List did not return secret")
	}

	if err := p.Unset("testapp", "DOPPLER_KEY"); err != nil {
		t.Fatal(err)
	}
	_, ok, _ = p.Get("testapp", "DOPPLER_KEY")
	if ok {
		t.Fatal("expected key to be deleted")
	}
}
