// Package chatapi exposes a local HTTP endpoint for programmatic chat access.
// Used by the Agent Benchmark Suite (ABS) and other local tooling.
// Only binds on 127.0.0.1 — never exposed externally.
package chatapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/persona"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
)

const defaultTimeout = 120 * time.Second

// Config holds the shared dependencies needed to build a pipeline for the Chat API.
type Config struct {
	AppConfig    *config.AppConfig
	Bridge       *bridge.Bridge
	Agents       *agents.Registry
	Persona      *persona.CanonicalIdentityService
	Sessions     *session.Store
	Tracker      *session.Tracker
	Resolver     *runtime.PathResolver
	MemoryDir    string
	ExePath      string
	BotCwd       string
	Orchestrator *orchestrator.Orchestrator
}

// Server is a local HTTP server that exposes POST /api/chat for synchronous chat.
type Server struct {
	pipeline   *pipelinepkg.Service
	output     *channelOutput
	httpServer *http.Server
	reqCounter int64

	// sessionKeys maps caller-supplied session keys to stable synthetic chatIDs,
	// so multi-turn scenarios retain conversation context across requests.
	sessionMu   sync.Mutex
	sessionKeys map[string]int64
}

// NewServer builds a Server bound to the given port using shared Aurelia deps.
func NewServer(port int, cfg Config) *Server {
	out := newChannelOutput()

	svc := pipelinepkg.NewService(pipelinepkg.Config{
		AppConfig:    cfg.AppConfig,
		Bridge:       cfg.Bridge,
		Agents:       cfg.Agents,
		Persona:      cfg.Persona,
		Sessions:     cfg.Sessions,
		Tracker:      cfg.Tracker,
		Resolver:     cfg.Resolver,
		MemoryDir:    cfg.MemoryDir,
		ExePath:      cfg.ExePath,
		BotCwd:       cfg.BotCwd,
		Output:       out,
		Orchestrator: cfg.Orchestrator,
	})

	s := &Server{
		pipeline:    svc,
		output:      out,
		sessionKeys: make(map[string]int64),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("GET /api/health", s.handleHealth)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	return s
}

// Start begins serving in the background. Returns immediately.
func (s *Server) Start() {
	go func() {
		slog.Info("chatapi: listening", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("chatapi: server error", "err", err)
		}
	}()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// chatID returns a stable synthetic chatID for the given session key,
// creating a new one if the key is not yet registered.
func (s *Server) chatID(sessionKey string) int64 {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if id, ok := s.sessionKeys[sessionKey]; ok {
		return id
	}
	id := atomic.AddInt64(&s.reqCounter, 1)
	s.sessionKeys[sessionKey] = id
	return id
}

type chatRequest struct {
	Text       string `json:"text"`
	SessionKey string `json:"session_key"`
}

type chatResponse struct {
	Response  string `json:"response"`
	LatencyMs int64  `json:"latency_ms"`
	ChatID    int64  `json:"chat_id"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Text == "" {
		http.Error(w, "text is required", http.StatusBadRequest)
		return
	}
	if req.SessionKey == "" {
		req.SessionKey = fmt.Sprintf("api-%d", atomic.AddInt64(&s.reqCounter, 1))
	}

	chatID := s.chatID(req.SessionKey)
	respCh, cleanup := s.output.register(chatID)
	defer cleanup()

	t0 := time.Now()
	if err := s.pipeline.Process(chatID, 0, 0, req.Text, nil); err != nil {
		http.Error(w, "pipeline error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), defaultTimeout)
	defer cancel()

	var response string
	select {
	case response = <-respCh:
	case <-ctx.Done():
		http.Error(w, "timeout waiting for pipeline response", http.StatusGatewayTimeout)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(chatResponse{
		Response:  response,
		LatencyMs: time.Since(t0).Milliseconds(),
		ChatID:    chatID,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
