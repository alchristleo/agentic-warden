//go:build !unix

package main

// rootOnly has no portable owner or permission bits to read off unix
// (Windows keeps them in ACLs the standard library cannot read), so it
// accepts every path; install-timer warns on Windows instead.
func rootOnly(string) error { return nil }
