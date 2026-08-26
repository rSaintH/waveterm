// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"strings"
	"testing"
)

func TestBuildArgsInteractive(t *testing.T) {
	args, err := BuildArgs(SessionOpts{AgentId: "claude", Interactive: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{"--print", "--output-format=stream-json", "--input-format=stream-json", "--verbose"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
}

// Without --verbose the CLI emits only the final result, which would look like a hang.
func TestBuildArgsAlwaysVerbose(t *testing.T) {
	args, err := BuildArgs(SessionOpts{AgentId: "claude", Prompt: "hi"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, a := range args {
		if a == "--verbose" {
			found = true
		}
	}
	if !found {
		t.Errorf("--verbose is required for streaming output, got %v", args)
	}
}

func TestBuildArgsOneShotNeedsPrompt(t *testing.T) {
	if _, err := BuildArgs(SessionOpts{AgentId: "claude"}); err == nil {
		t.Errorf("expected an error when a one-shot session has no prompt")
	}
}

// The prompt must be a separate argv entry, never spliced into a shell string.
func TestBuildArgsPromptIsSeparateArg(t *testing.T) {
	prompt := `it's "quoted" && dangerous`
	args, err := BuildArgs(SessionOpts{AgentId: "claude", Prompt: prompt})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args[len(args)-1] != prompt {
		t.Errorf("prompt should be passed verbatim as its own arg, got %q", args[len(args)-1])
	}
}

// An installed but undriveable agent has to fail with the reason, so the UI can explain it
// rather than appearing broken.
func TestBuildArgsUnsupportedAgentExplains(t *testing.T) {
	_, err := BuildArgs(SessionOpts{AgentId: "codex", Prompt: "hi"})
	if err == nil {
		t.Fatalf("expected an error for an unsupported agent")
	}
	if !strings.Contains(err.Error(), "Codex") {
		t.Errorf("error should name the agent, got %v", err)
	}
}

func TestBuildArgsUnknownAgent(t *testing.T) {
	if _, err := BuildArgs(SessionOpts{AgentId: "nope", Prompt: "hi"}); err == nil {
		t.Errorf("expected an error for an unknown agent id")
	}
}

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
		t.Errorf("empty and \"local\" are the local machine")
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
// "not installed" from "installed but unsupported".
func TestDetectAgentsReportsWholeCatalog(t *testing.T) {
	got, err := DetectAgents(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(Catalog) {
		t.Fatalf("got %d entries, want %d", len(got), len(Catalog))
	}
	for _, d := range got {
		if d.Id == "" || d.Label == "" {
			t.Errorf("entry is missing identity: %+v", d)
		}
	}
}

// An unsupported entry without a reason would leave the UI with nothing to say.
func TestUnsupportedCatalogEntriesExplainWhy(t *testing.T) {
	for _, def := range Catalog {
		if !def.Supported && def.Note == "" {
			t.Errorf("%s is unsupported but has no note", def.Id)
		}
	}
}
