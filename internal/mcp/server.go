// Package mcp is a small Model Context Protocol server: JSON-RPC 2.0 messages, one per line, over a
// reader and a writer (standard input and output for dirdoc mcp). It offers tools only.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// ProtocolVersions are the MCP revisions the server speaks, newest first.
var ProtocolVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}

// Tool is a tool the server offers. Call receives the arguments of a call as JSON and returns the
// text of the result and whether it reports an error.
type Tool struct {
	Name        string
	Title       string
	Description string
	InputSchema json.RawMessage
	ReadOnly    bool // the tool does not change anything
	Destructive bool // the tool may delete or overwrite what it cannot restore
	Idempotent  bool // calling it again with the same arguments changes nothing more
	Call        func(args json.RawMessage) (text string, isError bool)
}

// Server answers MCP requests with its tools.
type Server struct {
	Name, Version string
	Instructions  string // how to use the tools, sent to the client when it connects
	Tools         []Tool
}

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve answers the messages read from r on w, one line each, until r ends.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	in := bufio.NewReader(r)
	for {
		line, err := in.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			if out := s.HandleMessage(line); out != nil {
				if _, werr := w.Write(append(out, '\n')); werr != nil {
					return werr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// HandleMessage answers one message (a request, a notification or a batch of them). It returns
// nil when there is nothing to answer.
func (s *Server) HandleMessage(msg []byte) []byte {
	if len(msg) > 0 && msg[0] == '[' {
		var batch []json.RawMessage
		if err := json.Unmarshal(msg, &batch); err != nil || len(batch) == 0 {
			return encode(errorResponse(nil, codeInvalidRequest, "invalid batch"))
		}
		var out []*response
		for _, m := range batch {
			if r := s.handle(m); r != nil {
				out = append(out, r)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return encode(out)
	}
	if r := s.handle(msg); r != nil {
		return encode(r)
	}
	return nil
}

func encode(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"cannot encode the response"}}`)
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

func errorResponse(id json.RawMessage, code int, message string) *response {
	if id == nil {
		id = json.RawMessage("null")
	}
	return &response{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}

func (s *Server) handle(msg json.RawMessage) *response {
	var req request
	if err := json.Unmarshal(msg, &req); err != nil {
		return errorResponse(nil, codeParse, "parse error: "+err.Error())
	}
	if req.Method == "" {
		if req.ID == nil {
			return nil
		}
		var reply struct {
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(msg, &reply) == nil && (reply.Result != nil || reply.Error != nil) {
			return nil // a response to a request of ours; the server sends none
		}
		return errorResponse(req.ID, codeInvalidRequest, "missing method")
	}
	result, rerr := s.call(req.Method, req.Params)
	if req.ID == nil {
		return nil // a notification
	}
	if rerr != nil {
		return &response{JSONRPC: "2.0", ID: req.ID, Error: rerr}
	}
	return &response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) call(method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if len(params) > 0 {
			json.Unmarshal(params, &p)
		}
		version := ProtocolVersions[0]
		for _, v := range ProtocolVersions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		result := map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
		if s.Instructions != "" {
			result["instructions"] = s.Instructions
		}
		return result, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.Tools))
		for _, t := range s.Tools {
			tool := map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
				"annotations": map[string]any{
					"readOnlyHint":    t.ReadOnly,
					"destructiveHint": t.Destructive,
					"idempotentHint":  t.Idempotent,
					"openWorldHint":   false,
				},
			}
			if t.Title != "" {
				tool["title"] = t.Title
				tool["annotations"].(map[string]any)["title"] = t.Title
			}
			tools = append(tools, tool)
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: "invalid params: " + err.Error()}
		}
		for _, t := range s.Tools {
			if t.Name != p.Name {
				continue
			}
			args := p.Arguments
			if len(args) == 0 || string(args) == "null" {
				args = json.RawMessage("{}")
			}
			text, isError := t.Call(args)
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": isError,
			}, nil
		}
		return nil, &rpcError{Code: codeInvalidParams, Message: "unknown tool: " + p.Name}
	}
	if strings.HasPrefix(method, "notifications/") {
		return map[string]any{}, nil // initialized, cancelled, ...: nothing to do
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found: " + method}
}
