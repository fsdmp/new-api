package exceltools

import "fmt"

// nativeScript runs inside the jail as ns-root (uid 0 mapped from host root —
// required for the mounts, see NativeRunner). It is unit-testable on every
// platform, which is why it lives outside the linux-only runner file.
//
// Verified live on the production host 2026-09-07 (python:3.11-slim rootfs):
//   - /bin/sh is dash and does NOT support `ulimit -u/-v` → must use bash;
//   - Go writes the child's uid/gid maps via self-write, which forces
//     /proc/self/setgroups = "deny" → setpriv MUST NOT touch groups:
//     --clear-groups fails, --keep-groups (a no-op on groups) is required;
//   - the CPU rlimit is set 5s WIDER than the wall clock on purpose: when a
//     CPU-bound loop hits it first it dies to SIGKILL with empty stderr, which
//     is indistinguishable from a crash. The gateway ctx kill (timeout+1s)
//     must be the enforcer so TimedOut is reported correctly.
func nativeScript(timeoutSec int) string {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	// ulimit units: -t seconds, -v KB (1GB), -u processes, -f 512-byte blocks
	// (32MB = 65536). Mounts happen while the process still holds userns
	// caps; tmpfs defaults to mode 1777 so nobody can write /tmp afterwards.
	return fmt.Sprintf(`mount -t tmpfs -o size=32m,nodev,nosuid tmpfs /tmp 2>/dev/null || true
mount -t proc -o nosuid,noexec proc /proc 2>/dev/null || true
cd /tmp
ulimit -t %d; ulimit -v 1048576; ulimit -u 64; ulimit -f 65536
exec setpriv --no-new-privs --bounding-set=-all --inh-caps=-all --ambient-caps=-all --reuid=65534 --regid=65534 --keep-groups %s -I -`,
		timeoutSec+5, pythonInRootfs)
}
