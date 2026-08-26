// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package wshserver

import (
	"context"
	"fmt"

	"github.com/wavetermdev/waveterm/pkg/aiagent"
	"github.com/wavetermdev/waveterm/pkg/panichandler"
	"github.com/wavetermdev/waveterm/pkg/wshrpc"
	"github.com/wavetermdev/waveterm/pkg/wshutil"
)

var agentSessions = aiagent.MakeSessionManager()

func (ws *WshServer) AiAgentListCommand(ctx context.Context, data wshrpc.AiAgentListData) ([]aiagent.DetectedAgent, error) {
	return aiagent.DetectAgents(ctx, data.Connection)
}

// AiAgentRunCommand starts a session and streams its events until the process exits. The
// session is registered before the first event is read, so a prompt sent immediately after
// the call cannot miss it.
func (ws *WshServer) AiAgentRunCommand(ctx context.Context, data wshrpc.AiAgentRunData) <-chan wshrpc.RespOrErrorUnion[aiagent.AgentEvent] {
	ch := make(chan wshrpc.RespOrErrorUnion[aiagent.AgentEvent], 32)
	if data.SessionId == "" {
		go func() {
			defer close(ch)
			ch <- wshutil.RespErr[aiagent.AgentEvent](fmt.Errorf("sessionid is required"))
		}()
		return ch
	}
	if existing := agentSessions.Get(data.SessionId); existing != nil {
		go func() {
			defer close(ch)
			ch <- wshutil.RespErr[aiagent.AgentEvent](fmt.Errorf("session %s is already running", data.SessionId))
		}()
		return ch
	}
	// Deliberately not tied to ctx: the rpc context ends with the call, while the session
	// must outlive it and be stopped explicitly with AiAgentStopCommand.
	sess, err := aiagent.StartSession(context.Background(), data.SessionId, aiagent.SessionOpts{
		AgentId:     data.AgentId,
		Cwd:         data.Cwd,
		Connection:  data.Connection,
		Prompt:      data.Prompt,
		Interactive: data.Interactive,
	})
	if err != nil {
		go func() {
			defer close(ch)
			ch <- wshutil.RespErr[aiagent.AgentEvent](err)
		}()
		return ch
	}
	agentSessions.Add(sess)
	go func() {
		defer func() {
			panichandler.PanicHandler("AiAgentRunCommand", recover())
		}()
		defer close(ch)
		defer agentSessions.Remove(sess.Id)
		for ev := range sess.Events() {
			ch <- wshrpc.RespOrErrorUnion[aiagent.AgentEvent]{Response: ev}
		}
		if exitErr := sess.ExitErr(); exitErr != nil {
			// A non-zero exit after a clean stream is still worth reporting: it is how an
			// auth failure or a crash shows up.
			ch <- wshutil.RespErr[aiagent.AgentEvent](fmt.Errorf("agent exited: %w", exitErr))
		}
	}()
	return ch
}

func (ws *WshServer) AiAgentSendCommand(ctx context.Context, data wshrpc.AiAgentSendData) error {
	sess := agentSessions.Get(data.SessionId)
	if sess == nil {
		return fmt.Errorf("no running session %s", data.SessionId)
	}
	return sess.Send(data.Text)
}

func (ws *WshServer) AiAgentStopCommand(ctx context.Context, sessionId string) error {
	if agentSessions.Get(sessionId) == nil {
		// Stopping an already-finished session is not an error: the UI can race the
		// process exiting on its own.
		return nil
	}
	agentSessions.Remove(sessionId)
	return nil
}
