// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeLine(t *testing.T, barr []byte) map[string]any {
	t.Helper()
	if barr[len(barr)-1] != '\n' {
		t.Fatalf("control lines must be newline terminated or the agent never reads them")
	}
	var m map[string]any
	if err := json.Unmarshal(barr[:len(barr)-1], &m); err != nil {
		t.Fatalf("not valid json: %v", err)
	}
	return m
}

func TestEncodeInterrupt(t *testing.T) {
	barr, err := EncodeInterrupt("req-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := decodeLine(t, barr)
	if m["type"] != "control_request" {
		t.Errorf("type = %v", m["type"])
	}
	if m["request_id"] != "req-1" {
		t.Errorf("request_id must be carried so the reply can be correlated, got %v", m["request_id"])
	}
	req := m["request"].(map[string]any)
	if req["subtype"] != "interrupt" {
		t.Errorf("subtype = %v", req["subtype"])
	}
	// Without this a queued prompt would still run after a stop.
	if req["cancel_queued"] != true {
		t.Errorf("cancel_queued should be set, got %v", req["cancel_queued"])
	}
}

func TestEncodeInterruptNeedsRequestId(t *testing.T) {
	if _, err := EncodeInterrupt(""); err == nil {
		t.Errorf("an empty request_id cannot be correlated and must be rejected")
	}
}

func TestEncodeSetPermissionMode(t *testing.T) {
	barr, err := EncodeSetPermissionMode("req-2", "acceptEdits")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req := decodeLine(t, barr)["request"].(map[string]any)
	if req["subtype"] != "set_permission_mode" {
		t.Errorf("subtype = %v", req["subtype"])
	}
	if req["mode"] != "acceptEdits" {
		t.Errorf("mode = %v", req["mode"])
	}
}

// A typo must fail here rather than reaching the agent and being ignored.
func TestEncodeSetPermissionModeRejectsUnknown(t *testing.T) {
	if _, err := EncodeSetPermissionMode("req", "yolo"); err == nil {
		t.Errorf("expected an error for an unknown mode")
	}
}

func TestPermissionModesAreTheDocumentedSet(t *testing.T) {
	// These are the choices the CLI accepts for --permission-mode; a mode missing here
	// cannot be selected in the UI.
	for _, want := range []string{"auto", "acceptEdits", "manual", "dontAsk", "plan", "bypassPermissions"} {
		if !IsValidPermissionMode(want) {
			t.Errorf("%q should be a valid mode", want)
		}
	}
	if IsValidPermissionMode("") {
		t.Errorf("empty is not a mode; it means leave the CLI default")
	}
}

func TestEncodeToolDecisionAllow(t *testing.T) {
	barr, err := EncodeToolDecision("req-3", true, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := decodeLine(t, barr)
	if m["type"] != "control_response" {
		t.Errorf("type = %v", m["type"])
	}
	resp := m["response"].(map[string]any)
	if resp["request_id"] != "req-3" {
		t.Errorf("request_id = %v", resp["request_id"])
	}
	inner := resp["response"].(map[string]any)
	if inner["behavior"] != "allow" {
		t.Errorf("behavior = %v, want allow", inner["behavior"])
	}
}

// A denial carries a message the agent can read, so it can react instead of just failing.
func TestEncodeToolDecisionDenyAlwaysHasMessage(t *testing.T) {
	barr, err := EncodeToolDecision("req-4", false, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	inner := decodeLine(t, barr)["response"].(map[string]any)["response"].(map[string]any)
	if inner["behavior"] != "deny" {
		t.Errorf("behavior = %v, want deny", inner["behavior"])
	}
	if inner["message"] == nil || inner["message"] == "" {
		t.Errorf("a deny must carry a reason, got %v", inner["message"])
	}
}

func TestParseCanUseToolRequest(t *testing.T) {
	line := `{"type":"control_request","request_id":"r9","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"/tmp/x"}}}`
	ev, req, ok := ParseControlLine([]byte(line))
	if !ok {
		t.Fatalf("a control_request must be recognised")
	}
	if ev.Kind != EventKind_ToolRequest {
		t.Errorf("kind = %v, want toolrequest", ev.Kind)
	}
	if req == nil || req.RequestId != "r9" || req.ToolName != "Write" {
		t.Fatalf("request not extracted: %+v", req)
	}
	if !strings.Contains(string(req.Input), "file_path") {
		t.Errorf("the tool input must be carried so the user can judge the request")
	}
}

func TestParseControlResponseError(t *testing.T) {
	line := `{"type":"control_response","response":{"subtype":"error","request_id":"r1","error":"nope"}}`
	ev, req, ok := ParseControlLine([]byte(line))
	if !ok {
		t.Fatalf("a control_response must be recognised")
	}
	if req != nil {
		t.Errorf("a response is not a tool request")
	}
	if ev.Kind != EventKind_ControlResponse || !ev.IsError || ev.Text != "nope" {
		t.Errorf("error not surfaced: %+v", ev)
	}
}

// Regular protocol lines must fall through to the message parser.
func TestParseControlLineIgnoresNormalMessages(t *testing.T) {
	if _, _, ok := ParseControlLine([]byte(lineAssistant)); ok {
		t.Errorf("an assistant line is not a control message")
	}
	if _, _, ok := ParseControlLine([]byte("not json")); ok {
		t.Errorf("garbage is not a control message")
	}
}

// The control channel shares the stream, so the main parser has to route it.
func TestStreamParserRoutesControlLines(t *testing.T) {
	line := `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Bash"}}`
	ev, err := ParseStreamJSONLine([]byte(line))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ev.Kind != EventKind_ToolRequest {
		t.Errorf("kind = %v, want toolrequest", ev.Kind)
	}
}

func TestEncodeProjectDir(t *testing.T) {
	// Verified against the directories the CLI actually created on disk.
	cases := map[string]string{
		`C:\Users\rafa\scratch`:     "C--Users-rafa-scratch",
		"/home/rafa/workspace/proj": "-home-rafa-workspace-proj",
		`C:\a\.claude\b`:            "C--a--claude-b",
	}
	for in, want := range cases {
		if got := EncodeProjectDir(in); got != want {
			t.Errorf("EncodeProjectDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListHistoryNeedsCwd(t *testing.T) {
	if _, err := ListHistory(nil, "", ""); err == nil {
		t.Errorf("expected an error without a working directory")
	}
}
