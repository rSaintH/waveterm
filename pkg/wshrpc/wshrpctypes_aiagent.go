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
	AiAgentHistoryCommand(ctx context.Context, data AiAgentHistoryData) ([]aiagent.HistorySession, error)
	AiAgentInterruptCommand(ctx context.Context, sessionId string) error
	AiAgentSetPermissionModeCommand(ctx context.Context, data AiAgentPermissionModeData) error
	AiAgentToolDecisionCommand(ctx context.Context, data AiAgentToolDecisionData) error
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
	// One of the CLI permission modes; empty leaves the CLI default.
	PermissionMode string `json:"permissionmode,omitempty"`
	// Past session to continue. The transcript comes from the agent CLI store.
	ResumeSessionId string `json:"resumesessionid,omitempty"`
}

type AiAgentHistoryData struct {
	Connection string `json:"connection,omitempty"`
	Cwd        string `json:"cwd"`
}

type AiAgentPermissionModeData struct {
	SessionId string `json:"sessionid"`
	Mode      string `json:"mode"`
}

// AiAgentToolDecisionData answers a can_use_tool request. Without an answer the agent treats
// the tool as denied, so the UI has to reply either way.
type AiAgentToolDecisionData struct {
	SessionId string `json:"sessionid"`
	RequestId string `json:"requestid"`
	Allow     bool   `json:"allow"`
	Message   string `json:"message,omitempty"`
}

type AiAgentSendData struct {
	SessionId string `json:"sessionid"`
	Text      string `json:"text"`
}
