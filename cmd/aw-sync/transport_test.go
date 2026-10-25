package main

import (
	"strings"
	"testing"
)

func TestEnrollRefusesPlainHTTP(t *testing.T) {
	var out strings.Builder
	err := run([]string{"enroll", "--server", "http://awd.example.com", "--token", "t", "--state-dir", t.TempDir()}, &out, false)
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v, want a refusal naming https", err)
	}
}
