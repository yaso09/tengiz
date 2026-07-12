package runtime

import (
	"testing"
)

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
}

func TestStubMethods(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}
