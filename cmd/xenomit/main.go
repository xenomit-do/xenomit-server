package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/xenomit/server/internal/app"
)

func main() {
	cfg := app.Config{
		GameAddr:   env("GAME_ADDR", ":8080"),
		ChatAddr:   env("CHAT_ADDR", ":9338"),
		PolicyAddr: env("POLICY_ADDR", ":843"),
		HTTPAddr:   env("HTTP_ADDR", ":8088"),
		AdminToken: os.Getenv("ADMIN_TOKEN"),
		ClientRoot: env("CLIENT_ROOT", "client"),
		GamePublic: os.Getenv("GAME_PUBLIC_ADDR"),
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	srv, err := app.Start(ctx, cfg)
	if err != nil {
		slog.Error("start", "err", err)
		os.Exit(1)
	}
	slog.Info("xenomit listening",
		"game", srv.Game, "chat", srv.Chat, "policy", srv.Policy, "http", srv.HTTP)
	<-ctx.Done()
	slog.Info("shutting down")
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
