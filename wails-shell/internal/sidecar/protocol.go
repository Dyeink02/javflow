package sidecar

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Package sidecar owns the compatibility protocol contract between the Wails
// shell and the legacy Node sidecar process.
//
// Ownership summary:
// 1) define the command/result/event packet schema for the sidecar protocol
// 2) centralize packet validation and protocol-level errors
// 3) keep transport contract definitions separate from sidecar lifecycle logic
//
// File map for maintainers:
// 1) sidecar protocol packet DTOs
// 2) packet validation and protocol error helpers
// 3) command/result/event shaping utilities
//
// Boundary rule:
// this file defines protocol shape only. Sidecar lifecycle, feature routing,
// and compatibility policy belong in manager/router layers instead.

const BridgeVersion = "bridge.v1"

type CommandPacket struct {
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Domain    string `json:"domain"`
	Action    string `json:"action"`
	TaskID    string `json:"taskId,omitempty"`
	Timestamp string `json:"timestamp"`
	Payload   any    `json:"payload,omitempty"`
}

type ResultPacket struct {
	Version   string          `json:"version"`
	Kind      string          `json:"kind"`
	ID        string          `json:"id"`
	Domain    string          `json:"domain"`
	Action    string          `json:"action"`
	TaskID    string          `json:"taskId,omitempty"`
	OK        bool            `json:"ok"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
	Error     *ErrorPacket    `json:"error"`
}

type EventPacket struct {
	Version   string          `json:"version"`
	Kind      string          `json:"kind"`
	Event     string          `json:"event"`
	Domain    string          `json:"domain"`
	Action    string          `json:"action,omitempty"`
	TaskID    string          `json:"taskId,omitempty"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

type ErrorPacket struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Retriable bool           `json:"retriable"`
	Details   map[string]any `json:"details,omitempty"`
}

func NewCommandPacket(id string, domain string, action string, payload any) CommandPacket {
	return CommandPacket{
		Version:   BridgeVersion,
		Kind:      "command",
		ID:        id,
		Domain:    domain,
		Action:    action,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Payload:   payload,
	}
}

func DecodeResult(line []byte) (ResultPacket, error) {
	var packet ResultPacket
	if err := json.Unmarshal(line, &packet); err != nil {
		return ResultPacket{}, err
	}
	return packet, nil
}

func DecodeEvent(line []byte) (EventPacket, error) {
	var packet EventPacket
	if err := json.Unmarshal(line, &packet); err != nil {
		return EventPacket{}, err
	}
	return packet, nil
}

func ResultError(packet ResultPacket) error {
	if packet.OK {
		return nil
	}

	if packet.Error == nil {
		return fmt.Errorf("sidecar 返回未知错误")
	}

	return errors.New(packet.Error.Message)
}
