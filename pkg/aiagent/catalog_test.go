// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"testing"
)

func TestWslDistroFromConn(t *testing.T) {
	cases := map[string]string{
		"wsl://Ubuntu": "Ubuntu",
		"wsl://Debian": "Debian",
		"local":        "",
		"":             "",
		"user@host":    "",
	}
	for conn, want := range cases {
		if got := WslDistroFromConn(conn); got != want {
			t.Errorf("WslDistroFromConn(%q) = %q, want %q", conn, got, want)
		}
	}
}

func TestIsLocalConn(t *testing.T) {
	if !IsLocalConn("") || !IsLocalConn("local") {
		t.Errorf("empty and \"local\" both mean this machine")
	}
	if IsLocalConn("wsl://Ubuntu") {
		t.Errorf("a wsl connection is not local")
	}
}

// Detection must refuse a connection it cannot resolve instead of reporting an empty list,
// which would read as "nothing installed".
func TestDetectAgentsRejectsSsh(t *testing.T) {
	if _, err := DetectAgents(context.Background(), "user@host"); err == nil {
		t.Errorf("expected an error for an ssh connection")
	}
}

// Every catalog entry is reported, installed or not, so the UI can distinguish
// "not installed" from "installed but not launchable".
func TestDetectAgentsReportsWholeCatalog(t *testing.T) {
	got, err := DetectAgents(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(Catalog) {
		t.Fatalf("got %d entries, want %d", len(got), len(Catalog))
	}
	for _, d := range got {
		if d.Id == "" || d.Label == "" || d.Bin == "" {
			t.Errorf("entry is missing identity: %+v", d)
		}
	}
}

// A found agent must carry a path: the panel shows it, and an empty one would look like a
// detection bug.
func TestDetectedAgentCarriesPathWhenFound(t *testing.T) {
	got, err := DetectAgents(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, d := range got {
		if d.Found && d.Path == "" {
			t.Errorf("%s was found but has no path", d.Id)
		}
		if !d.Found && d.Path != "" {
			t.Errorf("%s was not found but has a path %q", d.Id, d.Path)
		}
	}
}

// Only Claude Code takes --permission-mode and --resume. Passing either to another CLI would
// stop it from starting, so the flags are per agent rather than assumed.
func TestOnlyClaudeDeclaresTheClaudeFlags(t *testing.T) {
	for _, def := range Catalog {
		if def.Id == "claude" {
			if !def.PermissionModeFlag || !def.ResumeFlag {
				t.Errorf("claude should declare both flags: %+v", def)
			}
			continue
		}
		if def.PermissionModeFlag || def.ResumeFlag {
			t.Errorf("%s must not claim claude's flags: %+v", def.Id, def)
		}
	}
}

// An agent that cannot be launched needs a reason, or the UI has nothing to say about it.
func TestUnsupportedCatalogEntriesExplainWhy(t *testing.T) {
	for _, def := range Catalog {
		if !def.Supported && def.Note == "" {
			t.Errorf("%s is unsupported but has no note", def.Id)
		}
	}
}

func TestEncodeProjectDir(t *testing.T) {
	// Verified against the directories the CLI actually created on disk.
	cases := map[string]string{
		`C:\Users\rafa\scratch`:     "C--Users-rafa-scratch",
		"/home/rafa/workspace/proj": "-home-rafa-workspace-proj",
		`C:\a\.claude\b`:            "C--a--claude-b",
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListHistoryNeedsCwd(t *testing.T) {
	if _, err := ListHistory(context.Background(), "", ""); err == nil {
		t.Errorf("expected an error without a working directory")
	}
}

func TestListHistoryRejectsSsh(t *testing.T) {
	if _, err := ListHistory(context.Background(), "user@host", "/tmp"); err == nil {
		t.Errorf("expected an error for an ssh connection")
	}
}

// A directory with no stored sessions is a normal new project, not a failure.
func TestListHistoryUnknownDirIsEmptyNotError(t *testing.T) {
	got, err := ListHistory(context.Background(), "", `C:\definitely\not\a\real\project\dir`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no sessions, got %d", len(got))
	}
}
