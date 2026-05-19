package webhook

import (
	"log/slog"
	"net/http"

	"github.com/livekit/protocol/auth"
	lkwebhook "github.com/livekit/protocol/webhook"

	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/recorder"
)

type OnRoomFinished func(livekitRoom string)
type OnEgressEnded func(egressID string, filePath string)

type Handler struct {
	keyProvider    *auth.SimpleKeyProvider
	manager        *recorder.Manager
	onRoomFinished OnRoomFinished
	onEgressEnded  OnEgressEnded
	log            *slog.Logger
}

func NewHandler(apiKey, apiSecret string, manager *recorder.Manager, onRoomFinished OnRoomFinished, onEgressEnded OnEgressEnded, log *slog.Logger) *Handler {
	return &Handler{
		keyProvider:    auth.NewSimpleKeyProvider(apiKey, apiSecret),
		manager:        manager,
		onRoomFinished: onRoomFinished,
		onEgressEnded:  onEgressEnded,
		log:            log,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	event, err := lkwebhook.ReceiveWebhookEvent(r, h.keyProvider)
	if err != nil {
		h.log.Error("failed to receive webhook", "error", err)
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		return
	}

	h.log.Info("webhook received", "event", event.GetEvent(), "room", event.GetRoom().GetName())

	switch event.GetEvent() {
	case lkwebhook.EventRoomFinished:
		if event.GetRoom() != nil {
			h.onRoomFinished(event.GetRoom().GetName())
		}
	case lkwebhook.EventEgressEnded:
		info := event.GetEgressInfo()
		if info != nil {
			var filePath string
			if results := info.GetFileResults(); len(results) > 0 {
				filePath = results[0].GetFilename()
			}
			h.onEgressEnded(info.GetEgressId(), filePath)
		}
	}

	w.WriteHeader(http.StatusOK)
}
