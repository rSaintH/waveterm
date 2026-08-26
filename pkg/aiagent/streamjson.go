// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

// Package aiagent talks to coding-agent CLIs over their stdio protocols so the GUI can host
// an agent session instead of leaving it to a raw terminal block.
//
// The only protocol implemented today is Claude Code's stream-json: newline-delimited JSON in
// both directions, enabled with --output-format=stream-json. It needs no adapter, unlike ACP,
// which no CLI installed on a typical machine speaks natively yet.
package aiagent

import (
	"encoding/json"
	"fmt"
)

type EventKind string

const (
	// Session started; carries the session id, cwd and the tool list.
	EventKind_Init EventKind = "init"
	// A message from the agent. Text is set for text blocks, ToolName for tool calls.
	EventKind_Assistant EventKind = "assistant"
	// A tool result being fed back to the agent.
	EventKind_ToolResult EventKind = "toolresult"
	// Turn finished. Carries the final text, cost and error state.
	EventKind_Result EventKind = "result"
	// Quota status. Worth surfacing: it is how a session dies without an error.
	EventKind_RateLimit EventKind = "ratelimit"
	// The agent is asking whether a tool may run (can_use_tool).
	EventKind_ToolRequest EventKind = "toolrequest"
	// Reply to a control request we sent (interrupt, set_permission_mode).
	EventKind_ControlResponse EventKind = "controlresponse"
	// A line we parsed as JSON but do not model. Kept rather than dropped so the UI can
	// still show something and new message types do not look like a hang.
	EventKind_Other EventKind = "other"
)

// AgentEvent is one decoded protocol line, flattened to what a UI actually needs.
type AgentEvent struct {
	Kind      EventKind `json:"kind"`
	SessionId string    `json:"sessionid,omitempty"`
	// Concatenated text blocks of an assistant message, or the final text on a result.
	Text string `json:"text,omitempty"`
	// Names of tools the agent called in this message, in order.
	ToolNames []string `json:"toolnames,omitempty"`
	Model     string   `json:"model,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`
	IsError   bool     `json:"iserror,omitempty"`
	// Set on a result line. "success", "error_max_turns", etc.
	Subtype string `json:"subtype,omitempty"`
	// Correlation id of a control message. Required to answer a tool request: without it
	// the UI has nothing to reply to.
	RequestId string `json:"requestid,omitempty"`
	// Raw tool input on a tool request, so the user can judge what is being asked.
	ToolInput json.RawMessage `json:"toolinput,omitempty"`
	// Populated on rate-limit lines: "allowed", "rejected", ...
	RateLimitStatus string  `json:"ratelimitstatus,omitempty"`
	CostUSD         float64 `json:"costusd,omitempty"`
	// The original line, so nothing is lost for debugging or future fields.
	Raw json.RawMessage `json:"raw,omitempty"`
}

// wire mirrors only the fields we read. Everything else stays in Raw.
type wire struct {
	Type      string  `json:"type"`
	Subtype   string  `json:"subtype"`
	SessionId string  `json:"session_id"`
	Cwd       string  `json:"cwd"`
	Model     string  `json:"model"`
	IsError   *bool   `json:"is_error"`
	Result    string  `json:"result"`
	CostUSD   float64 `json:"total_cost_usd"`
	Message   *struct {
		Model string `json:"model"`
		// Deliberately raw: content is an array of blocks on assistant lines but a plain
		// string on user lines, so a typed field here fails to unmarshal one of them.
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	RateLimitInfo *struct {
		Status string `json:"status"`
	} `json:"rate_limit_info"`
}

// ParseStreamJSONLine decodes one protocol line. A line that is not valid JSON is an error;
// a valid line of an unmodelled type comes back as EventKind_Other rather than an error,
// because the agent adding a message type must not break the session.
func ParseStreamJSONLine(line []byte) (AgentEvent, error) {
	// The control channel shares the stream, so it has to be recognised first: a
	// control_request has no "type" we would otherwise model.
	if ev, _, ok := ParseControlLine(line); ok {
		return ev, nil
	}
	var w wire
	if err := json.Unmarshal(line, &w); err != nil {
		return AgentEvent{}, fmt.Errorf("not valid stream-json: %w", err)
	}
	ev := AgentEvent{
		SessionId: w.SessionId,
		Subtype:   w.Subtype,
		Raw:       json.RawMessage(line),
	}
	switch {
	case w.Type == "system" && w.Subtype == "init":
		ev.Kind = EventKind_Init
		ev.Cwd = w.Cwd
		ev.Model = w.Model
	case w.Type == "assistant":
		ev.Kind = EventKind_Assistant
		if w.Message != nil {
			ev.Model = w.Message.Model
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
				Name string `json:"name"`
			}
			// A malformed content array is not fatal: the message still tells the UI the
			// agent is working, and Raw keeps whatever we could not read.
			if err := json.Unmarshal(w.Message.Content, &blocks); err == nil {
				for _, c := range blocks {
					switch c.Type {
					case "text":
						ev.Text += c.Text
					case "tool_use":
						ev.ToolNames = append(ev.ToolNames, c.Name)
					}
				}
			}
		}
	case w.Type == "user":
		ev.Kind = EventKind_ToolResult
	case w.Type == "result":
		ev.Kind = EventKind_Result
		ev.Text = w.Result
		ev.CostUSD = w.CostUSD
		if w.IsError != nil {
			ev.IsError = *w.IsError
		}
	case w.Type == "rate_limit_event":
		ev.Kind = EventKind_RateLimit
		if w.RateLimitInfo != nil {
			ev.RateLimitStatus = w.RateLimitInfo.Status
		}
	default:
		ev.Kind = EventKind_Other
	}
	return ev, nil
}

// UserPrompt is a line written to the agent's stdin when running with
// --input-format=stream-json.
type UserPrompt struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
}

// EncodeUserPrompt builds the stdin line that sends a prompt to a running session.
func EncodeUserPrompt(text string) ([]byte, error) {
	var p UserPrompt
	p.Type = "user"
	p.Message.Role = "user"
	p.Message.Content = text
	barr, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	return append(barr, '\n'), nil
}
