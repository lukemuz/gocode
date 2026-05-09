// Package web implements a stdlib-only web_fetch tool: download a URL,
// convert HTML to plain text, and return a slice of the result with optional
// pagination. Intended for documentation lookups and inspecting URLs the
// model encounters in error messages.
//
// Safety guards: http/https only, configurable timeout, body size cap, and
// redirect limit. There is no host allow/deny list — operators who need to
// keep the agent off internal networks should run with `-bash restricted`
// and consider running gocode itself in a sandbox.
package web

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/lukemuz/gocode"
)

const (
	defaultTimeout      = 10 * time.Second
	defaultMaxBodyBytes = 5 << 20 // 5 MiB
	defaultMaxRedirects = 5
	defaultMaxLength    = 8000
	defaultUserAgent    = "gocode-web-fetch/1.0 (+https://github.com/lukemuz/gocode)"
)

// Config controls Fetcher behaviour. Zero values pick sensible defaults.
type Config struct {
	Timeout      time.Duration
	MaxBodyBytes int64
	MaxRedirects int
	UserAgent    string
}

// Fetcher implements the web_fetch tool.
type Fetcher struct {
	client    *http.Client
	maxBody   int64
	userAgent string
}

// New constructs a Fetcher.
func New(cfg Config) *Fetcher {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	maxBody := cfg.MaxBodyBytes
	if maxBody == 0 {
		maxBody = defaultMaxBodyBytes
	}
	maxRedirects := cfg.MaxRedirects
	if maxRedirects == 0 {
		maxRedirects = defaultMaxRedirects
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = defaultUserAgent
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-http(s) scheme %q rejected", req.URL.Scheme)
			}
			return nil
		},
	}
	return &Fetcher{client: client, maxBody: maxBody, userAgent: ua}
}

// Toolset returns a single-binding toolset registering web_fetch.
func (f *Fetcher) Toolset() gocode.Toolset {
	schema := gocode.InputSchema{
		Type: "object",
		Properties: map[string]gocode.SchemaProperty{
			"url":         {Type: "string", Description: "Absolute http or https URL to fetch."},
			"max_length":  {Type: "integer", Description: "Maximum number of characters of body text to return (default 8000). The response indicates whether more content is available; paginate with start_index."},
			"start_index": {Type: "integer", Description: "0-indexed character offset into the (post-conversion) body. Use to page through long pages."},
			"raw":         {Type: "boolean", Description: "If true, return the raw response body without HTML→text conversion. Useful for JSON, XML, or plain text endpoints."},
		},
		Required: []string{"url"},
	}
	desc := "Fetch a URL over http(s) and return its content as text. HTML is converted to a plain-text approximation (script/style stripped, entities decoded, whitespace collapsed). Long pages are paginated via max_length + start_index. Set raw=true for non-HTML content (JSON, plaintext, etc.). When the response is an image (image/* content type), the text payload is a one-line metadata summary and the image bytes are attached to the result so the model receives them as visual content."
	tool, fn := gocode.NewTypedTool("web_fetch", desc, schema, f.handle)
	return gocode.Tools(gocode.ToolBinding{Tool: tool, Func: fn, Meta: gocode.ToolMetadata{RequiresConfirmation: false}})
}

type fetchInput struct {
	URL        string `json:"url"`
	MaxLength  int    `json:"max_length,omitempty"`
	StartIndex int    `json:"start_index,omitempty"`
	Raw        bool   `json:"raw,omitempty"`
}

func (f *Fetcher) handle(ctx context.Context, in fetchInput) (string, error) {
	if in.URL == "" {
		return "", fmt.Errorf("web_fetch: url is required")
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return "", fmt.Errorf("web_fetch: invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("web_fetch: only http and https are supported (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("web_fetch: url has no host")
	}

	maxLen := in.MaxLength
	if maxLen <= 0 {
		maxLen = defaultMaxLength
	}
	if in.StartIndex < 0 {
		return "", fmt.Errorf("web_fetch: start_index must be >= 0")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("web_fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/json,text/plain,*/*;q=0.8")

	resp, err := f.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("web_fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBody+1))
	if err != nil {
		return "", fmt.Errorf("web_fetch: read body: %w", err)
	}
	truncated := int64(len(body)) > f.maxBody

	contentType := resp.Header.Get("Content-Type")
	if mime := imageMIME(contentType, body); mime != "" && !in.Raw {
		// Image bytes can't be safely truncated mid-stream; bail rather
		// than emit a corrupt data URI.
		if truncated {
			return "", fmt.Errorf("web_fetch: image %s exceeds %d byte cap", u.String(), f.maxBody)
		}
		gocode.AttachImage(ctx, gocode.ImageBlock{
			Source:    "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body),
			MediaType: mime,
		})
		header := fmt.Sprintf("URL: %s\nStatus: %d %s\nContent-Type: %s\nBytes-fetched: %d", u.String(), resp.StatusCode, http.StatusText(resp.StatusCode), contentType, len(body))
		summary := fmt.Sprintf("image: %s (%d B)", mime, len(body))
		if resp.StatusCode >= 400 {
			return "", fmt.Errorf("%s\n\n%s", header, summary)
		}
		return header + "\n\n" + summary, nil
	}

	if truncated {
		body = body[:f.maxBody]
	}

	var text string
	if in.Raw || !looksHTML(contentType, body) {
		text = string(body)
	} else {
		text = htmlToText(body)
	}

	total := len(text)
	start := in.StartIndex
	if start > total {
		start = total
	}
	end := start + maxLen
	if end > total {
		end = total
	}
	slice := text[start:end]

	header := fmt.Sprintf("URL: %s\nStatus: %d %s\nContent-Type: %s\nBytes-fetched: %d", u.String(), resp.StatusCode, http.StatusText(resp.StatusCode), contentType, len(body))
	if truncated {
		header += "\nNote: response body exceeded the size cap and was truncated."
	}
	if total > end {
		header += fmt.Sprintf("\nPagination: returned chars [%d, %d) of %d. Call again with start_index=%d for more.", start, end, total, end)
	} else if start > 0 {
		header += fmt.Sprintf("\nPagination: returned chars [%d, %d) of %d. End of content.", start, end, total)
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("%s\n\n%s", header, slice)
	}
	return header + "\n\n" + slice, nil
}

// imageMIME returns the canonical image media type for a response, or ""
// if it doesn't look like an image. Header takes precedence; falls back
// to sniffing the body via http.DetectContentType so servers that omit or
// misreport Content-Type still get classified correctly.
func imageMIME(contentType string, body []byte) string {
	ct := strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	sniffLen := len(body)
	if sniffLen > 512 {
		sniffLen = 512
	}
	sniffed := http.DetectContentType(body[:sniffLen])
	if i := strings.Index(sniffed, ";"); i >= 0 {
		sniffed = sniffed[:i]
	}
	sniffed = strings.TrimSpace(sniffed)
	if strings.HasPrefix(sniffed, "image/") {
		return sniffed
	}
	return ""
}

func looksHTML(contentType string, body []byte) bool {
	ct := strings.ToLower(contentType)
	if strings.Contains(ct, "html") {
		return true
	}
	if strings.Contains(ct, "json") || strings.Contains(ct, "xml") || strings.Contains(ct, "plain") {
		return false
	}
	// Sniff a leading run of the body.
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	lower := strings.ToLower(strings.TrimSpace(string(head)))
	return strings.HasPrefix(lower, "<!doctype html") || strings.HasPrefix(lower, "<html")
}

var (
	scriptStyleRe = regexp.MustCompile(`(?is)<(script|style)\b[^>]*>.*?</(script|style)>`)
	commentRe     = regexp.MustCompile(`(?s)<!--.*?-->`)
	blockTagRe    = regexp.MustCompile(`(?i)<(/?(p|div|br|li|ul|ol|tr|h[1-6]|section|article|header|footer|pre|blockquote|hr))\b[^>]*>`)
	anyTagRe      = regexp.MustCompile(`<[^>]+>`)
	whitespaceRe  = regexp.MustCompile(`[ \t]+`)
	multiNewline  = regexp.MustCompile(`\n{3,}`)
)

// htmlToText is a deliberately small HTML→text converter. It drops
// <script>/<style> blocks and comments, inserts newlines around block
// elements, removes other tags, decodes entities, and collapses whitespace.
// It loses link targets and structural detail but preserves prose well
// enough for documentation lookups.
func htmlToText(body []byte) string {
	s := string(body)
	s = scriptStyleRe.ReplaceAllString(s, "")
	s = commentRe.ReplaceAllString(s, "")
	s = blockTagRe.ReplaceAllString(s, "\n")
	s = anyTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = whitespaceRe.ReplaceAllString(s, " ")
	// Trim trailing spaces from each line, then collapse blank-line runs.
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	s = strings.Join(lines, "\n")
	s = multiNewline.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// MarshalToolInput is exposed for tests.
func MarshalToolInput(in fetchInput) (json.RawMessage, error) {
	return json.Marshal(in)
}
