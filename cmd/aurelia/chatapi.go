package main

import (
	"context"
	"log"
	"os"

	"github.com/igormaneschy/aurelia/internal/chatapi"
)

// startChatAPI launches the Chat API server if ChatAPIPort > 0 in config.
func (a *app) startChatAPI() {
	if a.config == nil || a.config.ChatAPIPort <= 0 {
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		log.Printf("chatapi: failed to resolve cwd: %v — using empty string", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		log.Printf("chatapi: failed to resolve exe path: %v — using empty string", err)
	}

	srv := chatapi.NewServer(a.config.ChatAPIPort, chatapi.Config{
		AppConfig: a.config,
		Bridge:    a.bridge,
		Agents:    a.agents,
		Persona:   a.persona,
		Sessions:  a.sessions,
		Tracker:   a.tracker,
		Resolver:  a.resolver,
		MemoryDir: a.resolver.Memory(),
		ExePath:   exePath,
		BotCwd:    cwd,
	})
	a.chatAPI = srv
	srv.Start()
}

// shutdownChatAPI gracefully stops the Chat API server.
func (a *app) shutdownChatAPI(ctx context.Context) {
	if a.chatAPI == nil {
		return
	}
	if err := a.chatAPI.Shutdown(ctx); err != nil {
		log.Printf("chatapi: shutdown error: %v", err)
	}
}
