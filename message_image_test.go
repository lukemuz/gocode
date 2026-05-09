package gocode

import (
	"context"
	"encoding/json"
	"testing"
)

func TestImageBlockRoundTrip(t *testing.T) {
	// A user message carrying text + image must marshal and unmarshal
	// back to an identical block sequence, with TypeImage decoded as a
	// typed block (not opaque Raw).
	original := NewUserMessageWithImages("here is the photo", []ImageBlock{
		{Source: "data:image/png;base64,AAAA", MediaType: "image/png"},
	})
	if len(original.Content) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(original.Content))
	}
	if original.Content[0].Type != TypeText || original.Content[0].Text != "here is the photo" {
		t.Errorf("text block wrong: %+v", original.Content[0])
	}
	if original.Content[1].Type != TypeImage || original.Content[1].Source == "" || original.Content[1].MediaType != "image/png" {
		t.Errorf("image block wrong: %+v", original.Content[1])
	}

	wire, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded Message
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Content) != 2 {
		t.Fatalf("decoded blocks: got %d, want 2", len(decoded.Content))
	}
	img := decoded.Content[1]
	if img.Type != TypeImage {
		t.Errorf("image type lost: %+v", img)
	}
	if img.Source != "data:image/png;base64,AAAA" {
		t.Errorf("source lost: %q", img.Source)
	}
	if img.MediaType != "image/png" {
		t.Errorf("media_type lost: %q", img.MediaType)
	}
	if len(img.Raw) != 0 {
		t.Errorf("image block went through Raw path, want typed: Raw=%s", img.Raw)
	}
}

func TestNewUserMessageWithImagesEmptyText(t *testing.T) {
	msg := NewUserMessageWithImages("", []ImageBlock{
		{Source: "data:image/jpeg;base64,QUJD", MediaType: "image/jpeg"},
	})
	if len(msg.Content) != 1 {
		t.Fatalf("expected only image block, got %d", len(msg.Content))
	}
	if msg.Content[0].Type != TypeImage {
		t.Errorf("expected TypeImage, got %q", msg.Content[0].Type)
	}
}

func TestToolResultMessageFlattensImages(t *testing.T) {
	// Image attachments on any ToolResult ride along on the same canonical
	// user-role message that carries the tool_result blocks. The wire
	// serializer is responsible for splitting them onto a sibling user
	// message when sending.
	results := []ToolResult{
		{
			ToolUseID: "tu_1",
			Content:   "image: a.png (image/png, 42 B)",
			Images: []ImageBlock{
				{Source: "data:image/png;base64,AA", MediaType: "image/png"},
			},
		},
		{ToolUseID: "tu_2", Content: "ok"},
	}
	msg := NewToolResultMessage(results)
	if msg.Role != RoleUser {
		t.Fatalf("role = %q, want user", msg.Role)
	}
	if len(msg.Content) != 3 {
		t.Fatalf("expected 2 tool_result + 1 image block, got %d: %+v", len(msg.Content), msg.Content)
	}
	if msg.Content[0].Type != TypeToolResult || msg.Content[0].ToolUseID != "tu_1" {
		t.Errorf("first block wrong: %+v", msg.Content[0])
	}
	if msg.Content[1].Type != TypeToolResult || msg.Content[1].ToolUseID != "tu_2" {
		t.Errorf("second block wrong: %+v", msg.Content[1])
	}
	if msg.Content[2].Type != TypeImage || msg.Content[2].MediaType != "image/png" {
		t.Errorf("image block wrong: %+v", msg.Content[2])
	}
}

func TestAttachImageNoOpOutsideAgentLoop(t *testing.T) {
	// AttachImage on a vanilla context (no sink installed) is a no-op,
	// so unit tests that invoke ToolFuncs directly don't panic.
	AttachImage(context.Background(), ImageBlock{Source: "x", MediaType: "image/png"})
}

func TestAttachImageRecordedOnSink(t *testing.T) {
	ctx, sink := withImageSink(context.Background())
	AttachImage(ctx, ImageBlock{Source: "data:image/png;base64,AA", MediaType: "image/png"})
	AttachImage(ctx, ImageBlock{Source: "data:image/jpeg;base64,QUI", MediaType: "image/jpeg"})
	imgs := sink.drain()
	if len(imgs) != 2 {
		t.Fatalf("expected 2 attached images, got %d", len(imgs))
	}
	if imgs[0].MediaType != "image/png" || imgs[1].MediaType != "image/jpeg" {
		t.Errorf("attached images out of order: %+v", imgs)
	}
	if remaining := sink.drain(); len(remaining) != 0 {
		t.Errorf("expected drain to clear sink, got %d remaining", len(remaining))
	}
}
