// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package aiagent

import (
	"encoding/json"
	"fmt"
)

// The stream-json protocol carries a second, request/response channel alongside the message
// stream: control_request / control_response, correlated by request_id. It runs in both
// directions. We use it to interrupt a turn and to change the permission mode without
// restarting, and the agent uses it to ask whether a tool may run.
//
// Shapes below were read out of the claude 2.1.241 binary, since none of this is documented.

const (
	ControlSubtype_Interrupt         = "interrupt"
	ControlSubtype_SetPermissionMode = "set_permission_mode"
	ControlSubtype_CanUseTool        = "can_use_tool"
	ControlSubtype_Initialize        = "initialize"
)

// Permission modes accepted by --permission-mode and by set_permission_mode.
var PermissionModes = []string{"auto", "acceptEdits", "manual", "dontAsk", "plan", "bypassPermissions"}

func IsValidPermissionMode(mode string) bool {
	for _, m := range PermissionModes {
		if m == mode {
			return true
		}
	}
	return false
}

type controlRequestEnvelope struct {
	Type      string `json:"type"`
	RequestId string `json:"request_id"`
	Request   any    `json:"request"`
}

type controlResponseEnvelope struct {
	Type     string `json:"type"`
	Response any    `json:"response"`
}

// EncodeInterrupt cancels the current turn. cancel_queued also drops prompts that were
// queued behind it, which is what a stop button should do.
func EncodeInterrupt(requestId string) ([]byte, error) {
	return encodeControlRequest(requestId, map[string]any{
		"subtype":       ControlSubtype_Interrupt,
		"cancel_queued": true,
	})
}

// EncodeSetPermissionMode changes the mode of a running session, so the user does not have
// to restart to go from asking to auto.
func EncodeSetPermissionMode(requestId string, mode string) ([]byte, error) {
	if !IsValidPermissionMode(mode) {
		return nil, fmt.Errorf("unknown permission mode %q", mode)
	}
	return encodeControlRequest(requestId, map[string]any{
		"subtype": ControlSubtype_SetPermissionMode,
		"mode":    mode,
	})
}

// EncodeToolDecision answers a can_use_tool request. Denying carries a message, which the
// agent shows to itself as the reason, so it can react instead of just failing.
func EncodeToolDecision(requestId string, allow bool, denyMessage string) ([]byte, error) {
	inner := map[string]any{}
	if allow {
		inner["behavior"] = "allow"
	} else {
		inner["behavior"] = "deny"
		if denyMessage == "" {
			denyMessage = "denied by the user"
		}
		inner["message"] = denyMessage
	}
	env := controlResponseEnvelope{
		Type: "control_response",
		Response: map[string]any{
			"subtype":    "success",
			"request_id": requestId,
			"response":   inner,
		},
	}
	barr, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append(barr, '\n'), nil
}

func encodeControlRequest(requestId string, request map[string]any) ([]byte, error) {
	if requestId == "" {
		return nil, fmt.Errorf("request_id is required to correlate the response")
	}
	env := controlRequestEnvelope{
		Type:      "control_request",
		RequestId: requestId,
		Request:   request,
	}
	barr, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append(barr, '\n'), nil
}

// ToolRequest is an inbound can_use_tool request, flattened for the UI.
type ToolRequest struct {
	RequestId string          `json:"requestid"`
	ToolName  string          `json:"toolname"`
	Input     json.RawMessage `json:"input,omitempty"`
}

type inboundControl struct {
	Type      string `json:"type"`
	RequestId string `json:"request_id"`
	Request   *struct {
		Subtype  string          `json:"subtype"`
		ToolName string          `json:"tool_name"`
		Input    json.RawMessage `json:"input"`
	} `json:"request"`
	Response *struct {
		Subtype   string `json:"subtype"`
		RequestId string `json:"request_id"`
		Error     string `json:"error"`
	} `json:"response"`
}

// ParseControlLine recognises the control channel. ok is false for anything that is not a
// control message, so the caller can fall through to the normal message parser.
func ParseControlLine(line []byte) (ev AgentEvent, req *ToolRequest, ok bool) {
	var ic inboundControl
	if err := json.Unmarshal(line, &ic); err != nil {
		return AgentEvent{}, nil, false
	}
	switch ic.Type {
	case "control_request":
		if ic.Request == nil {
			return AgentEvent{}, nil, false
		}
		if ic.Request.Subtype != ControlSubtype_CanUseTool {
			// Some other inbound request we do not handle. Reported so it is visible
			// rather than looking like a stall.
			return AgentEvent{
				Kind:    EventKind_Other,
				Subtype: ic.Request.Subtype,
				Raw:     json.RawMessage(line),
			}, nil, true
		}
		return AgentEvent{
				Kind:      EventKind_ToolRequest,
				Subtype:   ic.Request.Subtype,
				ToolNames: []string{ic.Request.ToolName},
				RequestId: ic.RequestId,
				ToolInput: ic.Request.Input,
				Raw:       json.RawMessage(line),
			}, &ToolRequest{
				RequestId: ic.RequestId,
				ToolName:  ic.Request.ToolName,
				Input:     ic.Request.Input,
			}, true
	case "control_response":
		ev := AgentEvent{Kind: EventKind_ControlResponse, Raw: json.RawMessage(line)}
		if ic.Response != nil {
			ev.Subtype = ic.Response.Subtype
			if ic.Response.Subtype == "error" {
				ev.IsError = true
				ev.Text = ic.Response.Error
			}
		}
		return ev, nil, true
	}
	return AgentEvent{}, nil, false
}
