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
	AiAgentHistoryCommand(ctx context.Context, data AiAgentHistoryData) ([]aiagent.HistorySession, error)
}

type AiAgentListData struct {
	// "" or "local" for this machine, "wsl://Ubuntu" for a distro.
	Connection string `json:"connection,omitempty"`
}

type AiAgentHistoryData struct {
	Connection string `json:"connection,omitempty"`
	AgentId    string `json:"agentid"`
	Cwd        string `json:"cwd"`
}
