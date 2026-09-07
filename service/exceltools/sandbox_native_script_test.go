package exceltools

import (
	"strings"
	"testing"
)

// TestNativeScript locks the security-critical tokens of the jail bootstrap
// script. Every element here was learned the hard way on the production host
// (2026-09-07) — see nativeScript's doc comment for the rationale.
func TestNativeScript(t *testing.T) {
	s := nativeScript(20)
	mustContain := []string{
		"ulimit -t 25",           // CPU rlimit = wall clock + 5s (ctx kill fires first)
		"ulimit -v 1048576",      // 1GB address space
		"ulimit -u 64",           // process count
		"ulimit -f 65536",        // 32MB file writes (512-byte blocks)
		"mount -t tmpfs -o size=32m,nodev,nosuid tmpfs /tmp",
		"mount -t proc -o nosuid,noexec proc /proc",
		"cd /tmp",
		"setpriv --no-new-privs", // blocks suid/file-cap elevation forever
		"--bounding-set=-all --inh-caps=-all --ambient-caps=-all",
		"--reuid=65534 --regid=65534 --keep-groups", // keep-groups: setgroups is denied
		pythonInRootfs + " -I -",                     // isolated-mode python on stdin
	}
	for _, tok := range mustContain {
		if !strings.Contains(s, tok) {
			t.Errorf("nativeScript missing %q\nscript:\n%s", tok, s)
		}
	}
	for _, forbidden := range []string{"--clear-groups", "--init-groups", "/bin/sh"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("nativeScript must not contain %q (setgroups denied / dash lacks ulimit -u)", forbidden)
		}
	}

	if s := nativeScript(0); !strings.Contains(s, "ulimit -t 35") {
		t.Errorf("timeoutSec<=0 should default to 30 (+5 CPU headroom), got:\n%s", s)
	}
}
