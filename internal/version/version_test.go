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

func TestClean(t *testing.T) {
	for _, c := range []struct {
		v    string
		want bool
	}{
		{"v1.2.3", true},
		{"v1.2.3-rc.1", true},
		{"v0.0.0-20261025084530-c23407d80d14", false},
		{"v1.2.4-0.20261025084530-c23407d80d14", false},
		{"v1.2.4-rc.1.0.20261025084530-c23407d80d14", false},
		{"v1.2.3+dirty", false},
		{"v1.2.4-0.20261025084530-c23407d80d14+dirty", false},
		{"v2.0.0+incompatible", false},
		{"(devel)", false},
		{"", false},
	} {
		if got := clean(c.v); got != c.want {
			t.Errorf("clean(%q) = %v, want %v", c.v, got, c.want)
		}
	}
}
