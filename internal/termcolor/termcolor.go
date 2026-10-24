// Package termcolor wraps short strings in ANSI SGR codes when the output
// stream is a terminal. Callers decide once per command and pass the result
// as on; with on false every wrapper returns its input unchanged.
package termcolor

import (
	"os"
	"runtime"
)

// goos is a var so tests can stand in for Windows.
var goos = runtime.GOOS

// Enabled reports whether f is a terminal that should get color: a character
// device, NO_COLOR unset or empty, TERM not "dumb", and not Windows, because
// old consoles without VT processing print the escape codes raw.
func Enabled(f *os.File) bool {
	if goos == "windows" || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func wrap(on bool, code, s string) string {
	if !on {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func Green(on bool, s string) string  { return wrap(on, "32", s) }
func Red(on bool, s string) string    { return wrap(on, "31", s) }
func Yellow(on bool, s string) string { return wrap(on, "33", s) }
func Bold(on bool, s string) string   { return wrap(on, "1", s) }
func Dim(on bool, s string) string    { return wrap(on, "2", s) }
