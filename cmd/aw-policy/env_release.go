//go:build !awtest

package main

// envOverrides is false in every build made without -tags awtest, which is
// every deployed binary: Claude Code runs the helper with the developer's
// environment, so honouring AW_POLICY_BUNDLE there would let a developer
// choose their own policy. The safe value is the default so that a
// packager who forgets the tag still ships the secure binary.
const envOverrides = false
