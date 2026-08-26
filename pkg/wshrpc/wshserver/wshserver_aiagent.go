// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshserver

import (
	"context"

	"github.com/wavetermdev/waveterm/pkg/aiagent"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
)

func (ws *WshServer) AiAgentListCommand(ctx context.Context, data wshrpc.AiAgentListData) ([]aiagent.DetectedAgent, error) {
	return aiagent.DetectAgents(ctx, data.Connection)
}

func (ws *WshServer) AiAgentHistoryCommand(ctx context.Context, data wshrpc.AiAgentHistoryData) ([]aiagent.HistorySession, error) {
	return aiagent.ListHistory(ctx, data.Connection, data.Cwd)
}
