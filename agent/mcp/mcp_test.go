package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/lukemuz/gocode/agent/mcp"
)

// fakeServerPath builds and returns the path to the fake MCP server binary
// used in integration tests. It is skipped if 'go build' is unavailable.
func fakeServerPath(t *testing.T) string {
	t.Helper()
	out := t.TempDir() + "/fake-mcp-server"
	cmd := exec.Command("go", "build", "-o", out, "./testdata/fakeserver")
	cmd.Dir = "."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot build fake server: %v", err)
	}
	return out
}

func TestConnectAndToolset(t *testing.T) {
	serverBin := fakeServerPath(t)

	ctx := context.Background()
	srv, err := mcp.Connect(ctx, mcp.Config{Command: serverBin})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer srv.Close()

	toolset, err := srv.Toolset(ctx)
	if err != nil {
		t.Fatalf("Toolset: %v", err)
	}
	if len(toolset.Bindings) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(toolset.Bindings))
	}
	b := toolset.Bindings[0]
	if b.Tool.Name != "echo" {
		t.Errorf("expected tool name %q, got %q", "echo", b.Tool.Name)
	}
	if b.Meta.Source != "mcp" {
		t.Errorf("expected source %q, got %q", "mcp", b.Meta.Source)
	}
}

func TestToolCall(t *testing.T) {
	serverBin := fakeServerPath(t)

	ctx := context.Background()
	srv, err := mcp.Connect(ctx, mcp.Config{Command: serverBin})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer srv.Close()

	toolset, err := srv.Toolset(ctx)
	if err != nil {
		t.Fatalf("Toolset: %v", err)
	}

	dispatch := toolset.Dispatch()
	fn, ok := dispatch["echo"]
	if !ok {
		t.Fatal("expected echo tool in dispatch")
	}

	input, _ := json.Marshal(map[string]string{"message": "hello mcp"})
	out, err := fn(ctx, input)
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	if out != "hello mcp" {
		t.Errorf("expected %q, got %q", "hello mcp", out)
	}
}

func TestClose(t *testing.T) {
	serverBin := fakeServerPath(t)

	ctx := context.Background()
	srv, err := mcp.Connect(ctx, mcp.Config{Command: serverBin})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second close should not panic or error.
	_ = srv.Close()
}

func TestConnectBadCommand(t *testing.T) {
	ctx := context.Background()
	_, err := mcp.Connect(ctx, mcp.Config{Command: "/nonexistent/binary-that-does-not-exist"})
	if err == nil {
		t.Fatal("expected error for bad command")
	}
}

func TestConnectEmptyCommand(t *testing.T) {
	ctx := context.Background()
	_, err := mcp.Connect(ctx, mcp.Config{})
	if err == nil {
		t.Fatal("expected error for empty command")
	}
}

// --- helper: write a line-delimited JSON-RPC server used by fakeServer tests

// fakeRPCServer is a minimal in-process MCP server for unit testing the
// transport without spawning a subprocess. It is wired via io.Pipe.
type fakeRPCServer struct {
	serverIn  *os.File
	serverOut *os.File
	scanner   *bufio.Scanner
}

func newFakeRPCPair(t *testing.T) (clientIn, clientOut *os.File, scanner *bufio.Scanner) {
	t.Helper()
	// We only need fakeServerPath for the subprocess tests above.
	// This helper is intentionally unused here but kept for future reference.
	return nil, nil, nil
}

// writeResponse is a helper for the fake server binary (see testdata/).
func writeResponse(w *bufio.Writer, id int64, result any) {
	type resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int64  `json:"id"`
		Result  any    `json:"result"`
	}
	line, _ := json.Marshal(resp{JSONRPC: "2.0", ID: id, Result: result})
	fmt.Fprintln(w, string(line))
	w.Flush()
}
