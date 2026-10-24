package termcolor

import (
	"os"
	"testing"
)

func TestEnabled(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if Enabled(w) {
		t.Error("a pipe is not a terminal; color must be off")
	}

	dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer dev.Close()
	// /dev/null is a character device: it stands in for a terminal here.
	if !Enabled(dev) {
		t.Error("a character device with TERM set and NO_COLOR empty should enable color")
	}
	t.Setenv("NO_COLOR", "1")
	if Enabled(dev) {
		t.Error("NO_COLOR=1 must disable color")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if Enabled(dev) {
		t.Error("TERM=dumb must disable color")
	}
}

func TestWrappers(t *testing.T) {
	cases := []struct {
		name string
		fn   func(bool, string) string
		want string
	}{
		{"green", Green, "\x1b[32mhi\x1b[0m"},
		{"red", Red, "\x1b[31mhi\x1b[0m"},
		{"yellow", Yellow, "\x1b[33mhi\x1b[0m"},
		{"bold", Bold, "\x1b[1mhi\x1b[0m"},
		{"dim", Dim, "\x1b[2mhi\x1b[0m"},
	}
	for _, c := range cases {
		if got := c.fn(false, "hi"); got != "hi" {
			t.Errorf("%s off = %q, want input unchanged", c.name, got)
		}
		if got := c.fn(true, "hi"); got != c.want {
			t.Errorf("%s on = %q, want %q", c.name, got, c.want)
		}
	}
}
