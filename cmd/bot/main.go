package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/config"
	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/layouts"
	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/matrix"
	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/nextcloud"
	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/recorder"
	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/webhook"
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

	layoutURL := cfg.Layouts.BaseURL

	lkClient := recorder.NewLiveKitClient(
		cfg.LiveKit.URL,
		cfg.LiveKit.APIKey,
		cfg.LiveKit.APISecret,
		cfg.Recording.OutputDir,
		layoutURL,
		cfg.Recording.MaxVideoHeight,
	)

	mgr := recorder.NewManager(lkClient, cfg.Recording.MaxConcurrent, log)

	var nc *nextcloud.Client
	if cfg.Nextcloud.URL != "" {
		nc = nextcloud.NewClient(
			cfg.Nextcloud.URL,
			cfg.Nextcloud.Username,
			cfg.Nextcloud.Password,
			cfg.Nextcloud.UploadDir,
			log,
		)
		log.Info("nextcloud integration enabled", "url", cfg.Nextcloud.URL, "upload_dir", cfg.Nextcloud.UploadDir)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mxClient, err := matrix.NewClient(ctx, cfg, mgr, nc, log)
	if err != nil {
		log.Error("failed to create matrix client", "error", err)
		os.Exit(1)
	}
	defer mxClient.Close()

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
		func(egressID string, filePath string) {
			mxClient.SendEgressEnded(egressID, filePath)
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

	if nc != nil && cfg.Nextcloud.RetentionDays > 0 {
		go nc.RunCleanupLoop(ctx, cfg.Nextcloud.RetentionDays)
	}

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
