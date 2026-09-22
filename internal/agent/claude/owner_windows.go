//go:build windows

package claude

import "io/fs"

// ownerUID has no answer on Windows. There are no uids there, and the SID
// that owns a file — along with the ACL that actually decides who may
// rewrite it — is reachable only through APIs this build does not carry. The
// caller reports that as unknown rather than as "owned by root", because a
// check that cannot run must not read as a check that passed.
func ownerUID(info fs.FileInfo) (uid int, known bool) { return 0, false }
