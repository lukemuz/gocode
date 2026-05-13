package mcp_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/lukemuz/luft/mcp"
)

// fakeServerPath builds and returns the path to the fake MCP server binary.
// The test is skipped if 'go build' is unavailable.
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
	if len(toolset.Bindings) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(toolset.Bindings))
	}
	b := toolset.Bindings[0]
	if b.Tool.Name != "echo" {
		t.Errorf("expected first tool name %q, got %q", "echo", b.Tool.Name)
	}
	if b.Meta.Source != "mcp" {
		t.Errorf("expected source %q, got %q", "mcp", b.Meta.Source)
	}
	// InputSchema should be non-empty JSON passed through from the server.
	if len(b.Tool.InputSchema) == 0 {
		t.Error("expected non-empty InputSchema")
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

func TestToolCallIsError(t *testing.T) {
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

	fn, ok := toolset.Dispatch()["fail"]
	if !ok {
		t.Fatal("expected fail tool in dispatch")
	}

	_, err = fn(ctx, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error from isError:true response")
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
