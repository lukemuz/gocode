package openai

import (
	"encoding/json"
	"testing"

	"github.com/lukemuz/luft"
)

func marshalUserContent(t *testing.T, msg openAIMessage) []byte {
	t.Helper()
	raw, err := json.Marshal(msg.Content)
	if err != nil {
		t.Fatalf("marshal user content: %v", err)
	}
	return raw
}

func TestToOpenAIMessagesPlainTextUnchanged(t *testing.T) {
	// Backward-compat: a text-only user message without cache_control
	// must serialize as a plain string, not a typed-parts array.
	msgs := toOpenAIMessages("", nil, []luft.Message{
		luft.NewUserMessage("hi"),
	}, false)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 wire message, got %d", len(msgs))
	}
	if s, ok := msgs[0].Content.(string); !ok || s != "hi" {
		t.Errorf("expected plain string 'hi', got %T %v", msgs[0].Content, msgs[0].Content)
	}
}

func TestToOpenAIMessagesUserImageProducesTypedParts(t *testing.T) {
	// A user message carrying text + one image must serialize to a typed-
	// parts content array: one text part and one image_url part with the
	// image_url.url matching the canonical Source.
	msg := luft.NewUserMessageWithImages("look at this", []luft.ImageBlock{
		{Source: "data:image/png;base64,AAAA", MediaType: "image/png"},
	})
	wire := toOpenAIMessages("", nil, []luft.Message{msg}, false)
	if len(wire) != 1 {
		t.Fatalf("expected 1 wire message, got %d", len(wire))
	}
	raw := marshalUserContent(t, wire[0])

	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatalf("expected typed-parts array, got %s: %v", raw, err)
	}
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts (text + image_url), got %d: %s", len(parts), raw)
	}
	if parts[0]["type"] != "text" || parts[0]["text"] != "look at this" {
		t.Errorf("first part wrong: %v", parts[0])
	}
	if parts[1]["type"] != "image_url" {
		t.Errorf("second part type = %v, want image_url", parts[1]["type"])
	}
	imgURL, ok := parts[1]["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("image_url part missing image_url object: %v", parts[1])
	}
	if imgURL["url"] != "data:image/png;base64,AAAA" {
		t.Errorf("image_url.url = %v, want data:image/png;base64,AAAA", imgURL["url"])
	}
}

func TestToOpenAIMessagesToolResultPlusImageSplitsCorrectly(t *testing.T) {
	// Mixed: a canonical user message carrying a tool_result + image
	// (the shape NewToolResultMessage produces when a tool attached an
	// image) must split into a role="tool" message and a role="user"
	// message whose content is a typed-parts array.
	canon := luft.NewToolResultMessage([]luft.ToolResult{
		{
			ToolUseID: "tu_1",
			Content:   "image: shot.png (image/png, 42 B)",
			Images: []luft.ImageBlock{
				{Source: "data:image/png;base64,QUJD", MediaType: "image/png"},
			},
		},
	})
	wire := toOpenAIMessages("", nil, []luft.Message{canon}, false)
	if len(wire) != 2 {
		t.Fatalf("expected 2 wire messages (tool + user), got %d", len(wire))
	}
	if wire[0].Role != "tool" || wire[0].ToolCallID != "tu_1" {
		t.Errorf("first wire msg should be the tool message: %+v", wire[0])
	}
	if s, ok := wire[0].Content.(string); !ok || s == "" {
		t.Errorf("tool message content should be a non-empty string, got %T %v", wire[0].Content, wire[0].Content)
	}
	if wire[1].Role != luft.RoleUser {
		t.Fatalf("second wire msg should be role=user, got %q", wire[1].Role)
	}

	raw := marshalUserContent(t, wire[1])
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatalf("expected typed-parts array on user message, got %s: %v", raw, err)
	}
	if len(parts) != 1 {
		t.Fatalf("expected 1 image_url part (no text on tool-result-only turn), got %d: %s", len(parts), raw)
	}
	if parts[0]["type"] != "image_url" {
		t.Errorf("part type = %v, want image_url", parts[0]["type"])
	}
}

func TestToOpenAIMessagesUserTextWithCacheStillUsesTypedParts(t *testing.T) {
	// Pre-existing single-part typed-parts path for cache_control must
	// still produce exactly one text part with cache_control attached.
	msg := luft.Message{
		Role: luft.RoleUser,
		Content: []luft.ContentBlock{
			{Type: luft.TypeText, Text: "long context", CacheControl: luft.Ephemeral()},
		},
	}
	wire := toOpenAIMessages("", nil, []luft.Message{msg}, true)
	if len(wire) != 1 {
		t.Fatalf("expected 1 wire message, got %d", len(wire))
	}
	raw := marshalUserContent(t, wire[0])
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		t.Fatalf("expected typed-parts array for cache-marked text, got %s: %v", raw, err)
	}
	if len(parts) != 1 || parts[0]["type"] != "text" {
		t.Fatalf("expected one text part, got %s", raw)
	}
	if _, ok := parts[0]["cache_control"]; !ok {
		t.Errorf("cache_control dropped: %v", parts[0])
	}
}
