// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package shellexec

import (
	"strings"
	"testing"
)

// The composed string is handed to `sh -c` on the far side, so a command with arguments has
// to reach the shell as one token. Unquoted it silently loses every argument: the shell takes
// the first word as the command and the rest become positional parameters.
func TestComposeRemoteCommandQuotesTheCommand(t *testing.T) {
	got := ComposeRemoteCommand("/bin/bash", nil, "claude --resume 6e97e346")
	want := "/bin/bash -c 'claude --resume 6e97e346'"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Regression guard for the actual failure: an argument must not end up outside the quotes,
// where it would be read as $0 instead of part of the command.
func TestComposeRemoteCommandKeepsArgsInsideQuotes(t *testing.T) {
	got := ComposeRemoteCommand("/bin/bash", nil, "claude --resume abc")
	idx := strings.Index(got, "-c ")
	if idx < 0 {
		t.Fatalf("no -c in %q", got)
	}
	rest := got[idx+3:]
	if !strings.HasPrefix(rest, "'") || !strings.HasSuffix(rest, "'") {
		t.Errorf("the command must be a single quoted token, got %q", rest)
	}
	if strings.Contains(rest[1:len(rest)-1], "'") {
		t.Errorf("unexpected quote inside the command token: %q", rest)
	}
}

// Shell options set by the caller (login, rcfile) still have to come before -c.
func TestComposeRemoteCommandKeepsShellOpts(t *testing.T) {
	got := ComposeRemoteCommand("/bin/bash", []string{"-l"}, "echo hi")
	want := "/bin/bash -l -c 'echo hi'"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// A single quote in the command must not break out of the quoting.
func TestComposeRemoteCommandEscapesQuotes(t *testing.T) {
	got := ComposeRemoteCommand("/bin/bash", nil, "echo it's fine")
	if !strings.Contains(got, `'"'"'`) {
		t.Errorf("a single quote must be escaped, got %q", got)
	}
}

// The caller's slice must not be modified: it is reused for other shells.
func TestComposeRemoteCommandDoesNotMutateOpts(t *testing.T) {
	opts := []string{"-l"}
	ComposeRemoteCommand("/bin/bash", opts, "echo hi")
	if len(opts) != 1 || opts[0] != "-l" {
		t.Errorf("opts was modified: %v", opts)
	}
}
