// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package shellexec

import (
	"strings"
	"testing"
)

func TestWslStartDirArgsNoCwd(t *testing.T) {
	got := wslStartDirArgs("")
	if len(got) != 1 || got[0] != "~" {
		t.Errorf("wslStartDirArgs(\"\") = %v, want [~]", got)
	}
}

// A project tab on a WSL connection must start in the project directory. The path lives inside
// the distro, so it has to reach wsl.exe exactly as configured.
func TestWslStartDirArgsWithCwd(t *testing.T) {
	const cwd = "/home/rafa/workspace/gest-o-de-campanha"
	got := wslStartDirArgs(cwd)
	if len(got) != 2 || got[0] != "--cd" || got[1] != cwd {
		t.Errorf("wslStartDirArgs(%q) = %v, want [--cd %s]", cwd, got, cwd)
	}
}

// Guards against reintroducing local path handling: a windows-style separator here would mean
// something rewrote the remote path before it got this far.
func TestWslStartDirArgsKeepsPathVerbatim(t *testing.T) {
	const cwd = "/home/rafa/dir with spaces/proj"
	got := wslStartDirArgs(cwd)
	if got[1] != cwd {
		t.Errorf("path was altered: %q", got[1])
	}
	if strings.Contains(got[1], string(rune(92))) {
		t.Errorf("remote path must not contain windows separators: %q", got[1])
	}
}
