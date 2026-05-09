package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lukemuz/gocode"
)

// minimalPNG is a 1×1 PNG used to exercise the image branch without
// pulling in real image fixtures. http.DetectContentType matches on the
// PNG magic bytes alone, so any valid header is sufficient.
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

func TestFetchImageAttachesAndReturnsMetadata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(minimalPNG)
	}))
	defer srv.Close()

	f := New(Config{})
	ctx, drain := gocode.WithImageSink(context.Background())
	out, err := f.handle(ctx, fetchInput{URL: srv.URL})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	if !strings.Contains(out, "image: image/png") {
		t.Errorf("expected image metadata in text payload, got: %s", out)
	}
	if !strings.Contains(out, "Status: 200") {
		t.Errorf("expected status header, got: %s", out)
	}

	imgs := drain()
	if len(imgs) != 1 {
		t.Fatalf("expected 1 attached image, got %d", len(imgs))
	}
	if imgs[0].MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", imgs[0].MediaType)
	}
	if !strings.HasPrefix(imgs[0].Source, "data:image/png;base64,") {
		t.Errorf("Source should be base64 data URI, got %q", imgs[0].Source)
	}
}

func TestFetchHTMLDoesNotAttachImage(t *testing.T) {
	// Backward-compat: the HTML→text path must not produce image
	// attachments, no matter what's in the body.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><p>hi</p></body></html>`))
	}))
	defer srv.Close()

	f := New(Config{})
	ctx, drain := gocode.WithImageSink(context.Background())
	if _, err := f.handle(ctx, fetchInput{URL: srv.URL}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if imgs := drain(); len(imgs) != 0 {
		t.Errorf("HTML response should not attach images, got %d", len(imgs))
	}
}

func TestFetchImageRespectsRawFlag(t *testing.T) {
	// raw=true should bypass the image branch and return the bytes as
	// plain text (existing escape hatch for callers who want the body).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(minimalPNG)
	}))
	defer srv.Close()

	f := New(Config{})
	ctx, drain := gocode.WithImageSink(context.Background())
	out, err := f.handle(ctx, fetchInput{URL: srv.URL, Raw: true})
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if strings.Contains(out, "image: image/png") {
		t.Errorf("raw=true should not produce image metadata payload, got: %s", out)
	}
	if imgs := drain(); len(imgs) != 0 {
		t.Errorf("raw=true should not attach images, got %d", len(imgs))
	}
}
