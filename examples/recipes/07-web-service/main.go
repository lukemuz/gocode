// Recipe 07-web-service: the smallest deploy-shaped HTTP server that fronts
// a luft Agent. One file, plain net/http, no framework.
//
//	POST /chat     — JSON in, JSON out
//	GET  /healthz  — liveness probe
//
// This is a starting point. Clone the directory, swap the system prompt and
// toolset for your own, ship the binary or the Dockerfile. The "graduating
// to production" section of the README lists the things this template
// intentionally leaves out (streaming, per-session locking, auth, rate
// limiting, graceful shutdown).
//
// Local run:
//
//	export ANTHROPIC_API_KEY=sk-ant-...
//	go run ./examples/recipes/07-web-service
//
//	curl -s localhost:8080/chat -H 'content-type: application/json' \
//	    -d '{"session_id":"alice","message":"what is 17 * 23?"}' | jq
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/lukemuz/luft"
	"github.com/lukemuz/luft/providers/anthropic"
	"github.com/lukemuz/luft/stores"
	"github.com/lukemuz/luft/tools/clock"
	"github.com/lukemuz/luft/tools/math"
)

// systemPrompt is the personality / instructions for your agent. Replace
// this with whatever your service should be — a support bot, a code
// reviewer, an SRE on-call buddy, etc.
const systemPrompt = "You are a helpful assistant. Use the clock and math tools when they would give a more accurate answer than guessing."

func main() {
	store, err := stores.NewFileStore(envOr("SESSIONS_DIR", filepath.Join(os.TempDir(), "luft-web-sessions")))
	if err != nil {
		log.Fatal(err)
	}

	provider, err := anthropic.NewProviderFromEnv()
	if err != nil {
		log.Fatal(err)
	}

	client, err := luft.New(luft.Config{
		Provider:  provider,
		Model:     luft.ModelHaiku,
		MaxTokens: 4096,
	})
	if err != nil {
		log.Fatal(err)
	}

	assistant := luft.Agent{
		Client:  client,
		System:  systemPrompt,
		Tools:   luft.MustJoin(clock.New().Toolset(), math.New().Toolset()),
		Context: luft.ContextManager{MaxTokens: 8000, KeepRecent: 20},
		MaxIter: 6,
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	http.HandleFunc("/chat", chatHandler(assistant, store))

	addr := ":" + envOr("PORT", "8080")
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

type chatRequest struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

type chatResponse struct {
	SessionID string `json:"session_id"`
	Reply     string `json:"reply"`
}

// chatHandler is the one place where the luft pattern lives:
// load session → append user message → Step → save session → return reply.
func chatHandler(assistant luft.Agent, store luft.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, err)
			return
		}
		if req.SessionID == "" || req.Message == "" {
			writeJSONError(w, http.StatusBadRequest, errors.New("session_id and message are required"))
			return
		}

		sess, err := luft.Load(r.Context(), store, req.SessionID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		sess.History = append(sess.History, luft.NewUserMessage(req.Message))
		result, err := assistant.Step(r.Context(), sess.History)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, err)
			return
		}
		sess.History = result.Messages

		if err := luft.Save(r.Context(), store, sess); err != nil {
			writeJSONError(w, http.StatusInternalServerError, err)
			return
		}

		writeJSON(w, http.StatusOK, chatResponse{
			SessionID: sess.ID,
			Reply:     result.FinalText(),
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
