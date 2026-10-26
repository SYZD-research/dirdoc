package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestServe(t *testing.T) {
	s := &Server{Name: "x", Version: "1", Tools: []Tool{{
		Name: "echo", InputSchema: json.RawMessage(`{"type":"object"}`), ReadOnly: true,
		Call: func(args json.RawMessage) (string, bool) { return string(args), strings.Contains(string(args), "fail") },
	}}}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"a","method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"fail":true}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo"}}`,
		`[{"jsonrpc":"2.0","id":4,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/cancelled"}]`,
		`{"jsonrpc":"2.0","id":5,"method":"resources/list"}`,
		`{"jsonrpc":"2.0","id":6,"result":{}}`,
		`{oops`,
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nope","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":8,"method":"tools/list"}`,
	}, "\n")
	var out strings.Builder
	if err := s.Serve(strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`{"jsonrpc":"2.0","id":"a","result":{"capabilities":{"tools":{"listChanged":false}},"protocolVersion":"2025-11-25","serverInfo":{"name":"x","version":"1"}}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"content":[{"text":"{\"fail\":true}","type":"text"}],"isError":true}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"content":[{"text":"{}","type":"text"}],"isError":false}}`,
		`[{"jsonrpc":"2.0","id":4,"result":{}}]`,
		`{"jsonrpc":"2.0","id":5,"error":{"code":-32601,"message":"method not found: resources/list"}}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"parse error: invalid character 'o' looking for beginning of object key string"}}`,
		`{"jsonrpc":"2.0","id":7,"error":{"code":-32602,"message":"unknown tool: nope"}}`,
		`{"jsonrpc":"2.0","id":8,"result":{"tools":[{"annotations":{"destructiveHint":false,"idempotentHint":false,"openWorldHint":false,"readOnlyHint":true},"description":"","inputSchema":{"type":"object"},"name":"echo"}]}}`,
	}
	got := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("responses:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
