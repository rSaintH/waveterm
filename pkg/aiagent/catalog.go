// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Protocol is how we drive an agent's stdio.
type Protocol string

const (
	// Claude Code's native newline-delimited JSON. No adapter needed.
	Protocol_StreamJSON Protocol = "streamjson"
	// Reserved: the Agent Client Protocol. Nothing we ship drives it yet, and no CLI
	// checked so far speaks it without an external adapter.
	Protocol_Acp Protocol = "acp"
)

// AgentDef is a coding-agent CLI we know how to look for.
type AgentDef struct {
	Id       string   `json:"id"`
	Label    string   `json:"label"`
	Bin      string   `json:"bin"`
	Protocol Protocol `json:"protocol"`
	// False when we can find the binary but cannot drive it yet. Reported to the user
	// instead of hidden, so "I have it installed but it is not listed" never happens.
	Supported bool `json:"supported"`
	// Why it is unsupported, shown in the UI.
	Note string `json:"note,omitempty"`
}

// Catalog is ordered: the first supported agent found is the sensible default.
//
// Verified against the binaries on a Windows + WSL machine in Aug 2026:
// claude 2.1.241 has --output-format=stream-json and no ACP flag; codex-cli 0.147.0 exposes
// mcp-server and an experimental app-server but no ACP and no stream-json.
var Catalog = []AgentDef{
	{
		Id:        "claude",
		Label:     "Claude Code",
		Bin:       "claude",
		Protocol:  Protocol_StreamJSON,
		Supported: true,
	},
	{
		Id:        "codex",
		Label:     "Codex CLI",
		Bin:       "codex",
		Protocol:  Protocol_Acp,
		Supported: false,
		Note:      "codex has no stream-json or ACP mode; only an MCP server and an experimental app-server",
	},
	{
		Id:        "gemini",
		Label:     "Gemini CLI",
		Bin:       "gemini",
		Protocol:  Protocol_Acp,
		Supported: false,
		Note:      "speaks ACP natively, which this fork does not drive yet",
	},
}

// DetectedAgent is one catalog entry resolved against a machine.
type DetectedAgent struct {
	AgentDef
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`
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
		det := DetectedAgent{AgentDef: def}
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

// SessionOpts describes a session to start.
type SessionOpts struct {
	AgentId string `json:"agentid"`
	// Working directory for the agent, on the target machine.
	Cwd string `json:"cwd,omitempty"`
	// Connection the agent runs on: "" for local, "wsl://Ubuntu", ...
	Connection string `json:"connection,omitempty"`
	// The first prompt. Required when Interactive is false.
	Prompt string `json:"prompt,omitempty"`
	// When true the session stays open and reads further prompts from stdin.
	Interactive bool `json:"interactive,omitempty"`
}

// BuildArgs returns the CLI arguments for a session. The binary itself is resolved
// separately, since on a wsl:// connection the command is wrapped by wsl.exe.
func BuildArgs(opts SessionOpts) ([]string, error) {
	def, err := lookupAgentDef(opts.AgentId)
	if err != nil {
		return nil, err
	}
	if !def.Supported {
		return nil, fmt.Errorf("%s cannot be driven yet: %s", def.Label, def.Note)
	}
	if def.Protocol != Protocol_StreamJSON {
		return nil, fmt.Errorf("protocol %q is not implemented", def.Protocol)
	}
	// --verbose is required for stream-json to emit anything beyond the final result.
	args := []string{"--print", "--output-format=stream-json", "--verbose"}
	if opts.Interactive {
		args = append(args, "--input-format=stream-json")
	} else {
		if opts.Prompt == "" {
			return nil, fmt.Errorf("a non-interactive session needs a prompt")
		}
		args = append(args, opts.Prompt)
	}
	return args, nil
}
