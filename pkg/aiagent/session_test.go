// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"strings"
	"testing"
)

// A local session runs the binary directly and sets the process cwd.
func TestBuildExecCommandLocal(t *testing.T) {
	cmd, err := BuildExecCommand(context.Background(), SessionOpts{
		AgentId: "claude",
		Cwd:     "C:/dev/projeto",
		Prompt:  "oi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd.Path, "claude") {
		t.Errorf("expected to run claude, got %q", cmd.Path)
	}
	if cmd.Dir != "C:/dev/projeto" {
		t.Errorf("cwd should be set on the process, got %q", cmd.Dir)
	}
	if strings.Contains(strings.Join(cmd.Args, " "), "wsl.exe") {
		t.Errorf("a local session must not go through wsl.exe")
	}
}

// A wsl:// session must be wrapped by wsl.exe with --cd, because the cwd is a path inside
// the distro. Setting it as the Windows process cwd would silently fall back to the home
// directory, which is the bug this whole path exists to avoid.
func TestBuildExecCommandWsl(t *testing.T) {
	cmd, err := BuildExecCommand(context.Background(), SessionOpts{
		AgentId:    "claude",
		Connection: "wsl://Ubuntu",
		Cwd:        "/home/rafa/workspace/portal-clerk",
		Prompt:     "oi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(cmd.Path, "wsl.exe") {
		t.Errorf("expected wsl.exe, got %q", cmd.Path)
	}
	if !strings.Contains(joined, "--cd /home/rafa/workspace/portal-clerk") {
		t.Errorf("cwd must be passed with --cd, got %v", cmd.Args)
	}
	if !strings.Contains(joined, "-d Ubuntu") {
		t.Errorf("distro must be selected, got %v", cmd.Args)
	}
	if cmd.Dir != "" {
		t.Errorf("the windows-side cwd must stay unset for a wsl session, got %q", cmd.Dir)
	}
	if !strings.Contains(joined, "--output-format=stream-json") {
		t.Errorf("protocol args must survive the wsl wrapping, got %v", cmd.Args)
	}
}

// Without a cwd there is no --cd to add, and wsl.exe must still be well formed.
func TestBuildExecCommandWslNoCwd(t *testing.T) {
	cmd, err := BuildExecCommand(context.Background(), SessionOpts{
		AgentId:    "claude",
		Connection: "wsl://Ubuntu",
		Prompt:     "oi",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "--cd") {
		t.Errorf("no cwd was given, so --cd must be absent: %v", cmd.Args)
	}
	if !strings.Contains(joined, "-d Ubuntu") {
		t.Errorf("distro must still be selected, got %v", cmd.Args)
	}
}

func TestBuildExecCommandRejectsSsh(t *testing.T) {
	_, err := BuildExecCommand(context.Background(), SessionOpts{
		AgentId:    "claude",
		Connection: "user@host",
		Prompt:     "oi",
	})
	if err == nil {
		t.Errorf("expected an error for an ssh connection")
	}
}

func TestSessionManagerLifecycle(t *testing.T) {
	m := MakeSessionManager()
	if m.Get("nope") != nil {
		t.Errorf("unknown id should return nil")
	}
	sess := &Session{Id: "s1", AgentId: "claude"}
	m.Add(sess)
	if m.Get("s1") != sess {
		t.Errorf("session should be retrievable after Add")
	}
	if ids := m.Ids(); len(ids) != 1 || ids[0] != "s1" {
		t.Errorf("Ids = %v, want [s1]", ids)
	}
	m.Remove("s1")
	if m.Get("s1") != nil {
		t.Errorf("session should be gone after Remove")
	}
	// Removing twice must not panic: cleanup paths can race with process exit.
	m.Remove("s1")
}

// Send on a non-interactive session is a caller error, not a silent no-op.
func TestSendRequiresInteractive(t *testing.T) {
	sess := &Session{Id: "s1"}
	if err := sess.Send("oi"); err == nil {
		t.Errorf("expected an error when the session has no stdin")
	}
}

func TestSendAfterCloseFails(t *testing.T) {
	sess := &Session{Id: "s1", closed: true}
	if err := sess.Send("oi"); err == nil {
		t.Errorf("expected an error on a closed session")
	}
}
