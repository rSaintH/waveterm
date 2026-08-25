// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wcore

import (
	"testing"

	"github.com/wavetermdev/waveterm/pkg/waveobj"
)

func firstBlockMeta(t *testing.T, layout PortableLayout) waveobj.MetaMapType {
	t.Helper()
	if len(layout) != 1 {
		t.Fatalf("expected 1 layout entry, got %d", len(layout))
	}
	entry := layout[0]
	if entry.BlockDef == nil {
		t.Fatalf("expected a blockdef on the initial layout entry")
	}
	if !entry.Focused {
		t.Errorf("expected the initial block to be focused")
	}
	if len(entry.IndexArr) != 1 || entry.IndexArr[0] != 0 {
		t.Errorf("expected IndexArr [0], got %v", entry.IndexArr)
	}
	return entry.BlockDef.Meta
}

func TestGetNewTabLayoutNoCwd(t *testing.T) {
	meta := firstBlockMeta(t, GetNewTabLayout("", ""))

	if got := meta[waveobj.MetaKey_View]; got != "term" {
		t.Errorf("view = %v, want term", got)
	}
	if got := meta[waveobj.MetaKey_Controller]; got != "shell" {
		t.Errorf("controller = %v, want shell", got)
	}
	if _, present := meta[waveobj.MetaKey_CmdCwd]; present {
		t.Errorf("cmd:cwd must be absent for a generic tab, got %v", meta[waveobj.MetaKey_CmdCwd])
	}
}

func TestGetNewTabLayoutWithCwd(t *testing.T) {
	const cwd = "C:/Users/rafa/Documents/claudetrabalhos/waveterm"
	meta := firstBlockMeta(t, GetNewTabLayout(cwd, ""))

	if got := meta[waveobj.MetaKey_View]; got != "term" {
		t.Errorf("view = %v, want term", got)
	}
	if got := meta[waveobj.MetaKey_Controller]; got != "shell" {
		t.Errorf("controller = %v, want shell", got)
	}
	if got := meta[waveobj.MetaKey_CmdCwd]; got != cwd {
		t.Errorf("cmd:cwd = %v, want %v", got, cwd)
	}
	if _, present := meta[waveobj.MetaKey_Connection]; present {
		t.Errorf("connection must stay unset for a local project")
	}
}

// A WSL or SSH project needs the connection alongside the cwd: the path only exists on
// that machine, so a local shell started there would fail to spawn.
func TestGetNewTabLayoutWithConnection(t *testing.T) {
	const cwd = "/home/rafa/workspace/portal-clerk"
	const conn = "wsl://Ubuntu"
	meta := firstBlockMeta(t, GetNewTabLayout(cwd, conn))

	if got := meta[waveobj.MetaKey_CmdCwd]; got != cwd {
		t.Errorf("cmd:cwd = %v, want %v", got, cwd)
	}
	if got := meta[waveobj.MetaKey_Connection]; got != conn {
		t.Errorf("connection = %v, want %v", got, conn)
	}
}

// A connection with no path is still valid: open the tab on that machine at its default dir.
func TestGetNewTabLayoutConnectionWithoutCwd(t *testing.T) {
	meta := firstBlockMeta(t, GetNewTabLayout("", "wsl://Ubuntu"))

	if _, present := meta[waveobj.MetaKey_CmdCwd]; present {
		t.Errorf("cmd:cwd must stay unset when no path is configured")
	}
	if got := meta[waveobj.MetaKey_Connection]; got != "wsl://Ubuntu" {
		t.Errorf("connection = %v, want wsl://Ubuntu", got)
	}
}

// Windows project paths are commonly written with backslashes in projects.json.
// The layout must pass them through untouched so the shell receives the literal path.
func TestGetNewTabLayoutWindowsBackslashCwd(t *testing.T) {
	cwd := "C:" + string(rune(92)) + "Users" + string(rune(92)) + "rafa"
	meta := firstBlockMeta(t, GetNewTabLayout(cwd, ""))

	if got := meta[waveobj.MetaKey_CmdCwd]; got != cwd {
		t.Errorf("cmd:cwd = %q, want %q", got, cwd)
	}
}

// Each call must build a fresh meta map, otherwise a project tab would leak its
// cmd:cwd into a later generic tab.
func TestGetNewTabLayoutDoesNotShareMeta(t *testing.T) {
	withCwd := firstBlockMeta(t, GetNewTabLayout("C:/dev/projeto1", ""))
	generic := firstBlockMeta(t, GetNewTabLayout("", ""))

	if _, present := generic[waveobj.MetaKey_CmdCwd]; present {
		t.Fatalf("generic tab inherited cmd:cwd from a previous project tab")
	}
	if got := withCwd[waveobj.MetaKey_CmdCwd]; got != "C:/dev/projeto1" {
		t.Fatalf("project tab lost its cmd:cwd, got %v", got)
	}
}
