// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// Package aiagent finds coding-agent CLIs and reads the session history they already keep,
// so the GUI can launch one into a terminal block with the right project context.
package aiagent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// SessionPlaceholder marks where a session id goes in an argv template.
const SessionPlaceholder = "{session}"

// AgentDef is a coding-agent CLI we know how to look for and launch.
type AgentDef struct {
	Id    string `json:"id"`
	Label string `json:"label"`
	Bin   string `json:"bin"`
	// Sessions run in a terminal block, so any interactive CLI qualifies. False only for an
	// agent we know cannot be launched that way. Reported rather than hidden, so "I have it
	// installed but it is not listed" never happens.
	Supported bool `json:"supported"`
	// Whether the CLI accepts --permission-mode, and the values it takes. Passing a flag a
	// CLI does not know stops it from starting, so this is per agent rather than assumed.
	PermissionModeFlag bool     `json:"permissionmodeflag"`
	PermissionModes    []string `json:"permissionmodes,omitempty"`
	// Argv templates for continuing a past session, with SessionPlaceholder standing in for
	// the id. A template rather than a prefix because the id is not always last: Claude Code
	// forks with --resume <id> --fork-session. Empty means the action is not wired.
	ResumeArgs []string `json:"resumeargs,omitempty"`
	ForkArgs   []string `json:"forkargs,omitempty"`
	// Whether this package can read the agent's stored sessions.
	HistorySupported bool `json:"historysupported"`
	// Anything the user should know about this agent.
	Note string `json:"note,omitempty"`
}

// Catalog is ordered: the first supported agent found is the sensible default.
//
// Running the agent in a terminal is what keeps this small. Driving a protocol would need
// one implementation per CLI (stream-json for claude, an experimental JSON-RPC app-server
// for codex, ACP for gemini); launching an interactive CLI needs none.
//
// Verified against claude 2.1.241 and codex-cli 0.147.0.
var Catalog = []AgentDef{
	{
		Id:                 "claude",
		Label:              "Claude Code",
		Bin:                "claude",
		Supported:          true,
		PermissionModeFlag: true,
		// The choices --permission-mode accepts. "manual" is last on purpose: with no
		// permission-prompt tool registered it denies rather than asks.
		PermissionModes:  []string{"auto", "acceptEdits", "plan", "dontAsk", "bypassPermissions", "manual"},
		ResumeArgs:       []string{"--resume", SessionPlaceholder},
		ForkArgs:         []string{"--resume", SessionPlaceholder, "--fork-session"},
		HistorySupported: true,
	},
	{
		Id:        "codex",
		Label:     "Codex CLI",
		Bin:       "codex",
		Supported: true,
		// codex-cli 0.147.0 has no --permission-mode; approvals and sandboxing live in its
		// own config.
		PermissionModeFlag: false,
		// Subcommands, not flags: `codex resume <id>` and `codex fork <id>`.
		ResumeArgs:       []string{"resume", SessionPlaceholder},
		ForkArgs:         []string{"fork", SessionPlaceholder},
		HistorySupported: true,
	},
	{
		Id:        "gemini",
		Label:     "Gemini CLI",
		Bin:       "gemini",
		Supported: true,
		// No placeholder: --resume with no id continues the most recent session. Resuming a
		// specific one is not wired because the store lives under a project hash whose
		// derivation could not be verified without the CLI installed, so listing sessions
		// would be a guess. Gemini has no documented fork.
		ResumeArgs:       []string{"--resume"},
		HistorySupported: false,
		Note:             "past sessions are not listed here; use /resume inside gemini",
	},
}

// DetectedAgent is one catalog entry resolved against a machine. Fields are spelled out
// rather than embedding AgentDef, because the typescript generator flattens embedded structs
// inconsistently.
type DetectedAgent struct {
	Id                 string   `json:"id"`
	Label              string   `json:"label"`
	Bin                string   `json:"bin"`
	Supported          bool     `json:"supported"`
	PermissionModeFlag bool     `json:"permissionmodeflag"`
	PermissionModes    []string `json:"permissionmodes,omitempty"`
	ResumeArgs         []string `json:"resumeargs,omitempty"`
	ForkArgs           []string `json:"forkargs,omitempty"`
	HistorySupported   bool     `json:"historysupported"`
	Note               string   `json:"note,omitempty"`
	Found              bool     `json:"found"`
	Path               string   `json:"path,omitempty"`
}

func lookupAgentDef(id string) (AgentDef, error) {
	for _, def := range Catalog {
		if def.Id == id {
			return def, nil
		}
	}
	return AgentDef{}, fmt.Errorf("unknown agent %q", id)
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
// difference between "not installed" and "installed but not launchable".
//
// Only local and wsl:// connections are resolved; ssh returns an error rather than silently
// claiming nothing is installed.
func DetectAgents(ctx context.Context, connName string) ([]DetectedAgent, error) {
	distro := WslDistroFromConn(connName)
	if !IsLocalConn(connName) && distro == "" {
		return nil, fmt.Errorf("agent detection is not implemented for connection %q", connName)
	}
	var wslPaths map[string]string
	if distro != "" {
		bins := make([]string, 0, len(Catalog))
		for _, def := range Catalog {
			bins = append(bins, def.Bin)
		}
		var err error
		wslPaths, err = lookupBinsInWsl(ctx, distro, bins)
		if err != nil {
			// A dead distro is an error, not "nothing is installed".
			return nil, fmt.Errorf("cannot look for agents in %s: %w", distro, err)
		}
	}
	rtn := make([]DetectedAgent, 0, len(Catalog))
	for _, def := range Catalog {
		det := DetectedAgent{
			Id:                 def.Id,
			Label:              def.Label,
			Bin:                def.Bin,
			Supported:          def.Supported,
			PermissionModeFlag: def.PermissionModeFlag,
			PermissionModes:    def.PermissionModes,
			ResumeArgs:         def.ResumeArgs,
			ForkArgs:           def.ForkArgs,
			HistorySupported:   def.HistorySupported,
			Note:               def.Note,
		}
		var path string
		if distro != "" {
			path = wslPaths[def.Bin]
		} else if p, err := exec.LookPath(def.Bin); err == nil {
			path = p
		}
		if path != "" {
			det.Found = true
			det.Path = path
		}
		rtn = append(rtn, det)
	}
	return rtn, nil
}

// lookupBinsInWsl resolves every catalog binary in one shell. A login shell is used on
// purpose (these CLIs are usually installed by nvm or into ~/.local/bin, neither of which is
// on PATH otherwise), but a login shell costs seconds to start, so one invocation resolves
// everything instead of one per binary — three lookups used to blow past the rpc timeout.
func lookupBinsInWsl(ctx context.Context, distro string, bins []string) (map[string]string, error) {
	var sb strings.Builder
	for _, bin := range bins {
		sb.WriteString(fmt.Sprintf(`printf '%%s\t%%s\n' %q "$(command -v -- %q)"; `, bin, bin))
	}
	out, err := runInDistro(ctx, distro, sb.String(), true)
	if err != nil {
		return nil, err
	}
	rtn := map[string]string{}
	for _, line := range splitLines(out) {
		fields := strings.SplitN(line, "\t", 2)
		if len(fields) == 2 && fields[1] != "" {
			rtn[fields[0]] = fields[1]
		}
	}
	return rtn, nil
}
