package matrix

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/vlad/matrix-recording-bot/internal/config"
	"github.com/vlad/matrix-recording-bot/internal/recorder"
)

type Client struct {
	mx      *mautrix.Client
	manager *recorder.Manager
	cfg     *config.Config
	log     *slog.Logger
}

func NewClient(cfg *config.Config, manager *recorder.Manager, log *slog.Logger) (*Client, error) {
	mx, err := mautrix.NewClient(cfg.Matrix.Homeserver, id.UserID(cfg.Matrix.UserID), cfg.Matrix.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("creating matrix client: %w", err)
	}
	return &Client{mx: mx, manager: manager, cfg: cfg, log: log}, nil
}

func (c *Client) Run(ctx context.Context) error {
	syncer := c.mx.Syncer.(*mautrix.DefaultSyncer)

	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		c.handleMessage(ctx, evt)
	})

	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		membership := evt.Content.AsMember().Membership
		if membership == event.MembershipInvite && id.UserID(evt.GetStateKey()) == id.UserID(c.cfg.Matrix.UserID) {
			c.log.Info("invited to room, joining", "room", evt.RoomID)
			_, err := c.mx.JoinRoomByID(ctx, evt.RoomID)
			if err != nil {
				c.log.Error("failed to join room", "room", evt.RoomID, "error", err)
			}
		}
	})

	c.log.Info("starting matrix sync")
	return c.mx.SyncWithContext(ctx)
}

func (c *Client) handleMessage(ctx context.Context, evt *event.Event) {
	body := evt.Content.AsMessage().Body
	sender := evt.Sender

	if sender == id.UserID(c.cfg.Matrix.UserID) {
		return
	}

	switch {
	case strings.HasPrefix(body, "/record-call"):
		c.handleRecordCall(ctx, evt.RoomID, sender, body)
	case body == "/record-stop":
		c.handleRecordStop(ctx, evt.RoomID)
	}
}

func (c *Client) handleRecordCall(ctx context.Context, roomID id.RoomID, sender id.UserID, body string) {
	parts := strings.Fields(body)
	modeStr := ""
	if len(parts) > 1 {
		modeStr = parts[1]
	}
	mode, ok := recorder.ParseMode(modeStr, c.cfg.Recording.DefaultMode)
	if !ok {
		c.sendText(ctx, roomID, "Неизвестный режим. Доступные: full, screen, voice")
		return
	}

	livekitRoom, err := c.getLiveKitRoomName(ctx, roomID)
	if err != nil {
		c.sendText(ctx, roomID, fmt.Sprintf("Не удалось найти активный звонок: %v", err))
		return
	}

	err = c.manager.StartRecording(ctx, string(roomID), livekitRoom, string(sender), mode)
	if err != nil {
		c.sendText(ctx, roomID, fmt.Sprintf("Ошибка запуска записи: %v", err))
		return
	}

	modeNames := map[recorder.Mode]string{
		recorder.ModeFull:   "full — видео всех участников",
		recorder.ModeScreen: "screen — демонстрация экрана",
		recorder.ModeVoice:  "voice — только аудио",
	}
	c.sendText(ctx, roomID, fmt.Sprintf("🔴 Запись началась (режим: %s)", modeNames[mode]))
}

func (c *Client) handleRecordStop(ctx context.Context, roomID id.RoomID) {
	session, err := c.manager.StopRecording(ctx, string(roomID))
	if err != nil {
		c.sendText(ctx, roomID, fmt.Sprintf("Ошибка остановки записи: %v", err))
		return
	}
	c.sendText(ctx, roomID, fmt.Sprintf("⏹ Запись остановлена. Длительность: %s", session.Duration()))
}

func (c *Client) SendRoomFinished(ctx context.Context, livekitRoom string) {
	session, err := c.manager.StopByLiveKitRoom(ctx, livekitRoom)
	if err != nil || session == nil {
		return
	}
	c.sendText(ctx, id.RoomID(session.RoomID), fmt.Sprintf("⏹ Звонок завершён, запись сохранена. Длительность: %s", session.Duration()))
}

func (c *Client) SendEgressEnded(egressID string) {
	session := c.manager.HandleEgressEnded(egressID)
	if session == nil {
		return
	}
	ctx := context.Background()
	c.sendText(ctx, id.RoomID(session.RoomID), fmt.Sprintf("⏹ Запись завершена. Длительность: %s", session.Duration()))
}

func (c *Client) getLiveKitRoomName(ctx context.Context, roomID id.RoomID) (string, error) {
	stateMap, err := c.mx.State(ctx, roomID)
	if err != nil {
		return "", fmt.Errorf("getting room state: %w", err)
	}

	// RoomStateMap is map[event.Type]map[string]*event.Event
	// Look for MatrixRTC call.member state events
	for evtType, stateKeys := range stateMap {
		if evtType.Type == "org.matrix.msc3401.call.member" || evtType.Type == "m.call.member" {
			for _, evt := range stateKeys {
				if evt.Content.Raw != nil {
					// Element Call uses the Matrix room ID as the LiveKit room name
					return string(roomID), nil
				}
			}
		}
	}

	return "", fmt.Errorf("no active call found in room")
}

func (c *Client) sendText(ctx context.Context, roomID id.RoomID, text string) {
	_, err := c.mx.SendText(ctx, roomID, text)
	if err != nil {
		c.log.Error("failed to send message", "room", roomID, "error", err)
	}
}
