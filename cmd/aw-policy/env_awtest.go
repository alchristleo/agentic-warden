//go:build awtest

package main

// envOverrides is true only under -tags awtest, so the e2e tests can point
// the helper at fixtures instead of the root-owned system directory. A
// binary built this way must never be installed; `aw doctor` warns if one
// is, and `aw-policy build-info` says which kind it is.
const envOverrides = true
