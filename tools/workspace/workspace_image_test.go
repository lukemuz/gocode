package workspace_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lukemuz/luft"
	"github.com/lukemuz/luft/tools/workspace"
)

// minimalPNG is a 1×1 transparent PNG. http.DetectContentType recognizes
// PNG by its leading magic bytes, so this is enough to drive the image
// path in read_file without depending on real image assets.
var minimalPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
	0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

func callReadFile(t *testing.T, ws *workspace.Workspace, path string) (string, []luft.ImageBlock) {
	t.Helper()
	fn, ok := ws.Dispatch()["read_file"]
	if !ok {
		t.Fatalf("dispatch missing read_file")
	}
	ctx, drain := luft.WithImageSink(context.Background())
	raw, _ := json.Marshal(map[string]any{"path": path})
	out, err := fn(ctx, raw)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	return out, drain()
}

func TestReadFileImageReturnsMetadataAndAttachesImage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "shot.png"), minimalPNG, 0644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	ws, err := workspace.NewReadOnly(workspace.Config{Root: root})
	if err != nil {
		t.Fatalf("NewReadOnly: %v", err)
	}

	out, imgs := callReadFile(t, ws, "shot.png")

	if !strings.HasPrefix(out, "image:") {
		t.Errorf("read_file text payload should be image metadata, got %q", out)
	}
	if !strings.Contains(out, "shot.png") || !strings.Contains(out, "image/png") {
		t.Errorf("metadata should mention path and mime, got %q", out)
	}

	if len(imgs) != 1 {
		t.Fatalf("expected 1 attached image, got %d", len(imgs))
	}
	img := imgs[0]
	if img.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", img.MediaType)
	}
	if !strings.HasPrefix(img.Source, "data:image/png;base64,") {
		t.Errorf("Source should be a base64 data URI, got %q", img.Source)
	}
}

func TestReadFileTextFileBehaviorUnchanged(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello world\n"), 0644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	ws, err := workspace.NewReadOnly(workspace.Config{Root: root})
	if err != nil {
		t.Fatalf("NewReadOnly: %v", err)
	}

	out, imgs := callReadFile(t, ws, "hello.txt")

	if out != "hello world\n" {
		t.Errorf("text payload changed: got %q, want %q", out, "hello world\n")
	}
	if len(imgs) != 0 {
		t.Errorf("text file should not attach images, got %d", len(imgs))
	}
}
