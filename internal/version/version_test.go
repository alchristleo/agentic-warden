package version

import "testing"

func TestVersionDefaultDev(t *testing.T) {
	// Under `go test` the main module version is "(devel)" or empty.
	if got := String(); got != "dev" {
		t.Errorf("String() = %q, want dev", got)
	}
}

func TestVersionStampedWins(t *testing.T) {
	Version = "v1.2.3"
	defer func() { Version = "" }()
	if got := String(); got != "v1.2.3" {
		t.Errorf("String() = %q, want v1.2.3", got)
	}
}
