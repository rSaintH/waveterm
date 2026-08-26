// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AgentDef is a coding-agent CLI we know how to look for.
type AgentDef struct {
	Id    string `json:"id"`
	Label string `json:"label"`
	Bin   string `json:"bin"`
	// Sessions run in a terminal block, so any interactive CLI qualifies. This is false only
	// for an agent we know cannot be launched that way. Reported rather than hidden, so
	// "I have it installed but it is not listed" never happens.
	Supported bool `json:"supported"`
	// Whether the CLI accepts --permission-mode. Claude Code does; the others manage
	// permissions their own way, and passing the flag would just fail to start.
	PermissionModeFlag bool `json:"permissionmodeflag"`
	// Whether the CLI accepts --resume <id>, which is what makes a past session clickable.
	ResumeFlag bool `json:"resumeflag"`
	// Anything the user should know about this agent here.
	Note string `json:"note,omitempty"`
}

// Catalog is ordered: the first supported agent found is the sensible default.
//
// Running the agent in a terminal is what makes this list short. Driving a protocol would
// need one implementation per CLI (stream-json for claude, an experimental JSON-RPC
// app-server for codex, ACP for gemini); launching an interactive CLI needs none.
var Catalog = []AgentDef{
	{
		Id:                 "claude",
		Label:              "Claude Code",
		Bin:                "claude",
		Supported:          true,
		PermissionModeFlag: true,
		ResumeFlag:         true,
	},
	{
		Id:        "codex",
		Label:     "Codex CLI",
		Bin:       "codex",
		Supported: true,
		// codex-cli 0.147.0 has no --permission-mode; sandbox and approval settings live in
		// its own config. Session listing is not wired either, since its transcripts are
		// not in the claude store this panel reads.
		Note: "permission mode and past sessions are managed by codex itself",
	},
	{
		Id:        "gemini",
		Label:     "Gemini CLI",
		Bin:       "gemini",
		Supported: true,
		Note:      "permission mode and past sessions are managed by gemini itself",
	},
}

// DetectedAgent is one catalog entry resolved against a machine. Fields are spelled out
// rather than embedding AgentDef, because the typescript generator flattens embedded structs
// inconsistently.
type DetectedAgent struct {
	Id                 string `json:"id"`
	Label              string `json:"label"`
	Bin                string `json:"bin"`
	Supported          bool   `json:"supported"`
	PermissionModeFlag bool   `json:"permissionmodeflag"`
	ResumeFlag         bool   `json:"resumeflag"`
	Note               string `json:"note,omitempty"`
	Found              bool   `json:"found"`
	Path               string `json:"path,omitempty"`
}

// WslDistroFromConn returns the distro for a wsl:// connection, or "" for anything else.
func WslDistroFromConn(connName string) string {
	if !strings.HasPrefix(connName, "wsl://") {
		return ""
	}
	return strings.TrimPrefix(connName, "wsl://")
}

// IsLocalConn reports whether the connection means "this machine".
func IsLocalConn(connName string) bool {
	return connName == "" || connName == "local"
}

// DetectAgents reports which catalog agents exist on the given connection. An agent that is
// not installed is returned with Found=false rather than omitted, so the UI can tell the
// difference between "not installed" and "installed but unsupported".
//
// Only local and wsl:// connections are resolved; ssh returns an error rather than
// silently claiming nothing is installed.
func DetectAgents(ctx context.Context, connName string) ([]DetectedAgent, error) {
	distro := WslDistroFromConn(connName)
	if !IsLocalConn(connName) && distro == "" {
		return nil, fmt.Errorf("agent detection is not implemented for connection %q", connName)
	}
	rtn := make([]DetectedAgent, 0, len(Catalog))
	for _, def := range Catalog {
		det := DetectedAgent{
			Id:                 def.Id,
			Label:              def.Label,
			Bin:                def.Bin,
			Supported:          def.Supported,
			PermissionModeFlag: def.PermissionModeFlag,
			ResumeFlag:         def.ResumeFlag,
			Note:               def.Note,
		}
		var path string
		var err error
		if distro != "" {
			path, err = lookupInWsl(ctx, distro, def.Bin)
		} else {
			path, err = exec.LookPath(def.Bin)
		}
		if err == nil && path != "" {
			det.Found = true
			det.Path = path
		}
		rtn = append(rtn, det)
	}
	return rtn, nil
}

// lookupInWsl resolves a binary inside a distro. A login shell is used on purpose: these
// CLIs are usually installed by nvm or into ~/.local/bin, neither of which is on PATH for a
// non-login shell.
func lookupInWsl(ctx context.Context, distro string, bin string) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "-e", "bash", "-lc", "command -v "+bin)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(strings.ReplaceAll(string(out), "\r", ""))
	if path == "" {
		return "", fmt.Errorf("%s not found in %s", bin, distro)
	}
	return path, nil
}
