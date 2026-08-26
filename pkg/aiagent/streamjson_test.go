// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import "testing"

// These lines are trimmed from a real `claude -p --output-format=stream-json` session
// (claude 2.1.241), so the parser is tested against the shape the CLI actually emits
// rather than one invented from documentation.
const (
	lineInit      = `{"type":"system","subtype":"init","cwd":"C:\\Users\\rafa\\scratch","session_id":"5ddeed98","tools":["Read","Write"],"model":"claude-opus-5[1m]","permissionMode":"default","claude_code_version":"2.1.241"}`
	lineRateLimit = `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour"},"session_id":"5ddeed98"}`
	lineAssistant = `{"type":"assistant","message":{"model":"claude-opus-5","role":"assistant","content":[{"type":"text","text":"ok"}]},"session_id":"5ddeed98"}`
	lineSummary   = `{"type":"system","subtype":"post_turn_summary","status_category":"review_ready","session_id":"5ddeed98"}`
	lineResult    = `{"is_error":false,"num_turns":1,"session_id":"5ddeed98","total_cost_usd":0.383542,"subtype":"success","result":"ok","type":"result"}`
)

func TestParseInit(t *testing.T) {
	ev, err := ParseStreamJSONLine([]byte(lineInit))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Kind != EventKind_Init {
		t.Errorf("kind = %v, want init", ev.Kind)
	}
	if ev.SessionId != "5ddeed98" {
		t.Errorf("sessionid = %q", ev.SessionId)
	}
	if ev.Model != "claude-opus-5[1m]" {
		t.Errorf("model = %q", ev.Model)
	}
	if ev.Cwd == "" {
		t.Errorf("cwd should be carried through so the UI can show where the agent runs")
	}
}

func TestParseAssistantText(t *testing.T) {
	ev, err := ParseStreamJSONLine([]byte(lineAssistant))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Kind != EventKind_Assistant {
		t.Errorf("kind = %v, want assistant", ev.Kind)
	}
	if ev.Text != "ok" {
		t.Errorf("text = %q, want ok", ev.Text)
	}
	if len(ev.ToolNames) != 0 {
		t.Errorf("no tools were called, got %v", ev.ToolNames)
	}
}

// Text and tool calls arrive in the same message, and the UI needs both.
func TestParseAssistantWithToolUse(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"m","content":[{"type":"text","text":"reading "},{"type":"tool_use","name":"Read","id":"t1"},{"type":"text","text":"now"},{"type":"tool_use","name":"Grep","id":"t2"}]},"session_id":"s"}`
	ev, err := ParseStreamJSONLine([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Text != "reading now" {
		t.Errorf("text blocks must concatenate in order, got %q", ev.Text)
	}
	if len(ev.ToolNames) != 2 || ev.ToolNames[0] != "Read" || ev.ToolNames[1] != "Grep" {
		t.Errorf("toolnames = %v, want [Read Grep]", ev.ToolNames)
	}
}

func TestParseResult(t *testing.T) {
	ev, err := ParseStreamJSONLine([]byte(lineResult))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Kind != EventKind_Result {
		t.Errorf("kind = %v, want result", ev.Kind)
	}
	if ev.IsError {
		t.Errorf("is_error was false on the wire")
	}
	if ev.Text != "ok" {
		t.Errorf("result text = %q", ev.Text)
	}
	if ev.CostUSD == 0 {
		t.Errorf("cost should be surfaced; a session can get expensive quietly")
	}
	if ev.Subtype != "success" {
		t.Errorf("subtype = %q, want success", ev.Subtype)
	}
}

// A rejected quota is how a session dies without looking like an error, so it must not be
// swallowed as an unmodelled line.
func TestParseRateLimit(t *testing.T) {
	ev, err := ParseStreamJSONLine([]byte(lineRateLimit))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Kind != EventKind_RateLimit {
		t.Errorf("kind = %v, want ratelimit", ev.Kind)
	}
	if ev.RateLimitStatus != "allowed" {
		t.Errorf("status = %q, want allowed", ev.RateLimitStatus)
	}
}

// An unmodelled message type must not fail the stream: the CLI adds types over time.
func TestParseUnknownTypeIsNotAnError(t *testing.T) {
	ev, err := ParseStreamJSONLine([]byte(lineSummary))
	if err != nil {
		t.Fatalf("a known-but-unmodelled line must not error: %v", err)
	}
	if ev.Kind != EventKind_Other {
		t.Errorf("kind = %v, want other", ev.Kind)
	}
	if ev.Subtype != "post_turn_summary" {
		t.Errorf("subtype should still be carried, got %q", ev.Subtype)
	}
	if len(ev.Raw) == 0 {
		t.Errorf("raw must be kept so nothing is lost")
	}
}

func TestParseGarbageIsAnError(t *testing.T) {
	if _, err := ParseStreamJSONLine([]byte("this is not json")); err == nil {
		t.Errorf("expected an error for a non-JSON line")
	}
}

func TestEncodeUserPrompt(t *testing.T) {
	barr, err := EncodeUserPrompt("olá")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if barr[len(barr)-1] != '\n' {
		t.Errorf("the line must be newline terminated or the agent never reads it")
	}
	// Round-trip through the parser's JSON to be sure it is well formed.
	ev, err := ParseStreamJSONLine(barr[:len(barr)-1])
	if err != nil {
		t.Fatalf("encoded prompt is not valid json: %v", err)
	}
	if ev.Kind != EventKind_ToolResult {
		// type "user" is what the CLI expects for a prompt; our parser maps it to the
		// tool-result kind, which is the same wire type.
		t.Errorf("kind = %v, want the user-line kind", ev.Kind)
	}
}
