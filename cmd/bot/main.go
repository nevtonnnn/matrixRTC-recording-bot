package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/livekit/protocol/livekit"

	"github.com/vlad/matrix-recording-bot/internal/config"
	"github.com/vlad/matrix-recording-bot/internal/layouts"
	"github.com/vlad/matrix-recording-bot/internal/matrix"
	"github.com/vlad/matrix-recording-bot/internal/recorder"
	"github.com/vlad/matrix-recording-bot/internal/webhook"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	layoutURL := fmt.Sprintf("http://localhost%s%sscreen.html", cfg.Layouts.Listen, cfg.Layouts.Path)

	lkClient := recorder.NewLiveKitClient(
		cfg.LiveKit.URL,
		cfg.LiveKit.APIKey,
		cfg.LiveKit.APISecret,
		cfg.Recording.OutputDir,
		layoutURL,
	)

	mgr := recorder.NewManager(lkClient, log)

	mxClient, err := matrix.NewClient(cfg, mgr, log)
	if err != nil {
		log.Error("failed to create matrix client", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mgr.RestoreFromEgress(ctx); err != nil {
		log.Warn("failed to restore sessions from egress", "error", err)
	}

	mux := http.NewServeMux()

	webhookHandler := webhook.NewHandler(
		cfg.LiveKit.APIKey,
		cfg.LiveKit.APISecret,
		mgr,
		func(livekitRoom string) {
			mxClient.SendRoomFinished(ctx, livekitRoom)
		},
		func(egressID string, info *livekit.EgressInfo) {
			mxClient.SendEgressEnded(egressID)
		},
		log,
	)
	mux.Handle(cfg.Webhook.Path, webhookHandler)

	mux.Handle(cfg.Layouts.Path, layouts.Handler(cfg.Layouts.Path))

	go func() {
		log.Info("starting HTTP server", "listen", cfg.Webhook.Listen)
		if err := http.ListenAndServe(cfg.Webhook.Listen, mux); err != nil {
			log.Error("HTTP server error", "error", err)
		}
	}()

	go func() {
		if err := mxClient.Run(ctx); err != nil {
			log.Error("matrix sync error", "error", err)
			cancel()
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Info("bot started",
		"homeserver", cfg.Matrix.Homeserver,
		"user", cfg.Matrix.UserID,
		"output_dir", cfg.Recording.OutputDir,
	)

	<-sigCh
	log.Info("shutting down")
	cancel()
}
