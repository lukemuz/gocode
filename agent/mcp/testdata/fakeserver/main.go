// Fake MCP server for testing the mcp package. It communicates over stdio
// using line-delimited JSON-RPC 2.0 and exposes a single "echo" tool that
// returns the "message" argument as its text content.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Result  any    `json:"result"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)

	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		// Notifications have no ID; ignore them.
		if req.ID == nil {
			continue
		}

		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.0.1"},
			}
		case "tools/list":
			result = map[string]any{
				"tools": []any{
					map[string]any{
						"name":        "echo",
						"description": "Echoes the message argument.",
						"inputSchema": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"message": map[string]any{
									"type":        "string",
									"description": "The text to echo.",
								},
							},
							"required": []string{"message"},
						},
					},
				},
			}
		case "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "bad params"}}}
				break
			}
			var args map[string]string
			if err := json.Unmarshal(params.Arguments, &args); err != nil {
				result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "bad args"}}}
				break
			}
			msg := args["message"]
			result = map[string]any{
				"isError": false,
				"content": []any{
					map[string]any{"type": "text", "text": msg},
				},
			}
		default:
			result = map[string]any{"error": fmt.Sprintf("unknown method %q", req.Method)}
		}

		resp := response{JSONRPC: "2.0", ID: *req.ID, Result: result}
		line, _ := json.Marshal(resp)
		fmt.Fprintln(writer, string(line))
		writer.Flush()
	}
}
