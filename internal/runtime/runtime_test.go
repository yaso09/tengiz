package runtime

import (
	"testing"
)

func TestNewStub(t *testing.T) {
	m := NewStub()
	if m == nil {
		t.Fatal("NewStub() returned nil")
	}
}

func TestStubSatisfiesInterface(t *testing.T) {
	m := NewStub()
	var iface Manager = m
	if iface == nil {
		t.Fatal("Manager interface not satisfied")
	}
}
