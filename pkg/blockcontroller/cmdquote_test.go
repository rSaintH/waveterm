// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package blockcontroller

import (
	"testing"

	"github.com/wavetermdev/waveterm/pkg/util/shellutil"
)

// A cmd path with spaces has to be quoted for the shell that will run it; on Windows the
// agent CLIs often live under paths like "C:\Users\John Doe\AppData\...".
func TestQuoteCmdPathForShell(t *testing.T) {
	cases := []struct {
		shellType string
		in        string
		want      string
	}{
		{shellutil.ShellType_pwsh, `C:\Program Files\claude.exe`, `& 'C:\Program Files\claude.exe'`},
		{shellutil.ShellType_bash, "/opt/my tools/claude", "'/opt/my tools/claude'"},
		{shellutil.ShellType_zsh, "/opt/my tools/claude", "'/opt/my tools/claude'"},
	}
	for _, c := range cases {
		got, err := quoteCmdPathForShell(c.in, c.shellType)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", c.shellType, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.shellType, got, c.want)
		}
	}
}

// With no known shell to quote for, the old explicit error is still better than handing the
// shell a command it will silently word-split.
func TestQuoteCmdPathForShellUnknownShellErrors(t *testing.T) {
	if _, err := quoteCmdPathForShell(`C:\Program Files\claude.exe`, shellutil.ShellType_unknown); err == nil {
		t.Errorf("expected an error for an unknown shell type")
	}
}
