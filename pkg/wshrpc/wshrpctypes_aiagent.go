// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// coding-agent types and methods for wsh rpc calls
package wshrpc

import (
	"context"

	"github.com/wavetermdev/waveterm/pkg/aiagent"
)

type WshRpcAiAgentInterface interface {
	AiAgentListCommand(ctx context.Context, data AiAgentListData) ([]aiagent.DetectedAgent, error)
	AiAgentRunCommand(ctx context.Context, data AiAgentRunData) <-chan RespOrErrorUnion[aiagent.AgentEvent]
	AiAgentSendCommand(ctx context.Context, data AiAgentSendData) error
	AiAgentStopCommand(ctx context.Context, sessionId string) error
}

type AiAgentListData struct {
	// "" or "local" for this machine, "wsl://Ubuntu" for a distro.
	Connection string `json:"connection,omitempty"`
}

// AiAgentRunData starts a session and streams it. The caller supplies the session id so it
// can send prompts without waiting for a reply first, which would race the session's own
// opening events.
type AiAgentRunData struct {
	SessionId   string `json:"sessionid"`
	AgentId     string `json:"agentid"`
	Connection  string `json:"connection,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	Interactive bool   `json:"interactive,omitempty"`
}

type AiAgentSendData struct {
	SessionId string `json:"sessionid"`
	Text      string `json:"text"`
}
