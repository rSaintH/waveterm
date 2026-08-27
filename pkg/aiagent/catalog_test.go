// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
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

// Only Claude Code takes --permission-mode. Passing it to another CLI would stop it from
// starting, so the flag is declared per agent rather than assumed.
func TestOnlyClaudeTakesPermissionMode(t *testing.T) {
	for _, def := range Catalog {
		if def.Id == "claude" {
			if !def.PermissionModeFlag || len(def.PermissionModes) == 0 {
				t.Errorf("claude should declare the flag and its modes: %+v", def)
			}
			continue
		}
		if def.PermissionModeFlag {
			t.Errorf("%s does not take --permission-mode: %+v", def.Id, def)
		}
	}
}

// Declaring modes without the flag, or the flag without modes, would give the UI a dropdown
// that cannot work.
func TestPermissionModesAndFlagAgree(t *testing.T) {
	for _, def := range Catalog {
		if def.PermissionModeFlag != (len(def.PermissionModes) > 0) {
			t.Errorf("%s: flag=%v but %d modes", def.Id, def.PermissionModeFlag, len(def.PermissionModes))
		}
	}
}

// Resume and fork argv differ per CLI: claude takes flags, codex takes subcommands, and
// the id is not always last. Getting this wrong means the agent does not start.
func TestSessionArgTemplates(t *testing.T) {
	want := map[string]struct{ resume, fork []string }{
		"claude": {
			resume: []string{"--resume", SessionPlaceholder},
			fork:   []string{"--resume", SessionPlaceholder, "--fork-session"},
		},
		"codex": {
			resume: []string{"resume", SessionPlaceholder},
			fork:   []string{"fork", SessionPlaceholder},
		},
		// No placeholder: --resume with no id continues the most recent session.
		"gemini": {resume: []string{"--resume"}, fork: nil},
	}
	same := func(a, b []string) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	for _, def := range Catalog {
		exp, ok := want[def.Id]
		if !ok {
			continue
		}
		if !same(def.ResumeArgs, exp.resume) {
			t.Errorf("%s resumeargs = %v, want %v", def.Id, def.ResumeArgs, exp.resume)
		}
		if !same(def.ForkArgs, exp.fork) {
			t.Errorf("%s forkargs = %v, want %v", def.Id, def.ForkArgs, exp.fork)
		}
	}
}

// A template that takes an id must say where it goes, or the id is silently dropped and the
// agent opens the wrong session.
func TestTemplatesWithAnIdCarryThePlaceholder(t *testing.T) {
	for _, def := range Catalog {
		for label, tmpl := range map[string][]string{"resume": def.ResumeArgs, "fork": def.ForkArgs} {
			if len(tmpl) < 2 {
				continue
			}
			found := false
			for _, a := range tmpl {
				if a == SessionPlaceholder {
					found = true
				}
			}
			if !found {
				t.Errorf("%s %s template has no placeholder: %v", def.Id, label, tmpl)
			}
		}
	}
}

// History is only claimed for stores this package can actually read.
func TestHistorySupportMatchesReaders(t *testing.T) {
	readable := map[string]bool{"claude": true, "codex": true}
	for _, def := range Catalog {
		if def.HistorySupported != readable[def.Id] {
			t.Errorf("%s: historysupported=%v but reader present=%v", def.Id, def.HistorySupported, readable[def.Id])
		}
		// An agent without history has to say why, or the panel shows an empty list with no
		// explanation.
		if !def.HistorySupported && def.Note == "" {
			t.Errorf("%s has no history and no note", def.Id)
		}
	}
}

// An agent that cannot be launched needs a reason, or the UI has nothing to say about it.
func TestListHistoryUnknownAgent(t *testing.T) {
	if _, err := ListHistory(context.Background(), "", "nope", "/tmp"); err == nil {
		t.Errorf("expected an error for an unknown agent id")
	}
}

func TestUnsupportedCatalogEntriesExplainWhy(t *testing.T) {
	for _, def := range Catalog {
		if !def.Supported && def.Note == "" {
			t.Errorf("%s is unsupported but has no note", def.Id)
		}
	}
}

func TestEncodeProjectDir(t *testing.T) {
	// Verified against the encoder bundled in claude 2.1.241: every character outside
	// [a-zA-Z0-9] becomes a dash, counted per UTF-16 code unit.
	cases := map[string]string{
		`C:\Users\rafa\scratch`:         "C--Users-rafa-scratch",
		"/home/rafa/workspace/proj":     "-home-rafa-workspace-proj",
		`C:\a\.claude\b`:                "C--a--claude-b",
		`C:\Users\rafa\my_project (v2)`: "C--Users-rafa-my-project--v2-",
		`C:\João`:                       "C--Jo-o",
		"ação é 😀":                      "a--o-----",
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// Past 200 characters the CLI truncates and appends a base36 hash of the raw path. The
// expected values were produced by the CLI's own hash function.
func TestEncodeProjectDirLongPathGetsHashSuffix(t *testing.T) {
	in := `C:\Users\rafa\` + strings.Repeat("x", 250)
	want := "C--Users-rafa-" + strings.Repeat("x", 186) + "-g0kg5j"
	if got := EncodeProjectDir(in); got != want {
		t.Errorf("EncodeProjectDir(long) = %q, want %q", got, want)
	}
}

func TestClaudePathHash(t *testing.T) {
	cases := map[string]string{
		`C:\Users\rafa\Documents\workspace-windows`: "8s9vyq",
		`C:\Users\rafa\my_project (v2)`:             "a7q2ti",
		"ação é 😀":                                  "uj4tyw",
	}
	for in, want := range cases {
		if got := claudePathHash(in); got != want {
			t.Errorf("claudePathHash(%q) = %q, want %q", in, got, want)
		}
	}
}

// The CLI encodes the cwd exactly as the process saw it, so on Windows a project launched
// with "c:" and with "C:" has two store directories; both have to be searched.
func TestClaudeProjectDirCandidatesFlipDriveCase(t *testing.T) {
	got := claudeProjectDirCandidates(`C:\Users\rafa`)
	if len(got) != 2 || got[0] != "C--Users-rafa" || got[1] != "c--Users-rafa" {
		t.Errorf("candidates = %v", got)
	}
	if got := claudeProjectDirCandidates("/home/rafa/proj"); len(got) != 1 {
		t.Errorf("a path without a drive letter has one candidate, got %v", got)
	}
}

func TestTruncateTitleCutsOnRuneBoundary(t *testing.T) {
	in := strings.Repeat("a", 79) + "ção mais texto para passar do limite"
	got := truncateTitle(in)
	if !utf8.ValidString(got) {
		t.Errorf("truncated title is not valid utf-8: %q", got)
	}
	if len(got) > 80+len("…") {
		t.Errorf("title was not truncated: %q", got)
	}
}

func TestListHistoryNeedsCwd(t *testing.T) {
	if _, err := ListHistory(context.Background(), "", "claude", ""); err == nil {
		t.Errorf("expected an error without a working directory")
	}
}

func TestListHistoryRejectsSsh(t *testing.T) {
	if _, err := ListHistory(context.Background(), "user@host", "claude", "/tmp"); err == nil {
		t.Errorf("expected an error for an ssh connection")
	}
}

// A directory with no stored sessions is a normal new project, not a failure.
func TestListHistoryUnknownDirIsEmptyNotError(t *testing.T) {
	got, err := ListHistory(context.Background(), "", "claude", `C:\definitely\not\a\real\project\dir`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no sessions, got %d", len(got))
	}
}
