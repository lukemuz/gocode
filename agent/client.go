package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const (
	defaultBaseURL     = "https://api.anthropic.com"
	anthropicVersion   = "2023-06-01"
	defaultMaxTokens   = 1024
	defaultHTTPTimeout = 60 * time.Second
)

// anthropicRequest is the JSON body sent to POST /v1/messages.
type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
}

// anthropicResponse is the parsed reply from the API.
type anthropicResponse struct {
	ID         string         `json:"id"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

// Usage records token consumption for one API call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type apiErrorBody struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) do(ctx context.Context, req anthropicRequest) (anthropicResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return anthropicResponse{}, fmt.Errorf("agent: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return anthropicResponse{}, fmt.Errorf("agent: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", c.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return anthropicResponse{}, fmt.Errorf("agent: http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody apiErrorBody
		json.NewDecoder(resp.Body).Decode(&errBody) //nolint:errcheck
		return anthropicResponse{}, &APIError{
			StatusCode: resp.StatusCode,
			Type:       errBody.Error.Type,
			Message:    errBody.Error.Message,
		}
	}

	var result anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return anthropicResponse{}, fmt.Errorf("agent: decode response: %w", err)
	}
	return result, nil
}
