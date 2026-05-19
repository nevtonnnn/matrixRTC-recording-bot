package matrix

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/config"
	"github.com/nevtonnnn/matrixRTC-recording-bot/internal/recorder"
)

type Client struct {
	mx           *mautrix.Client
	cryptoHelper *cryptohelper.CryptoHelper
	manager      *recorder.Manager
	cfg          *config.Config
	log          *slog.Logger
}

func NewClient(ctx context.Context, cfg *config.Config, manager *recorder.Manager, log *slog.Logger) (*Client, error) {
	mx, err := mautrix.NewClient(cfg.Matrix.Homeserver, "", "")
	if err != nil {
		return nil, fmt.Errorf("creating matrix client: %w", err)
	}

	helper, err := cryptohelper.NewCryptoHelper(mx, []byte(cfg.Matrix.PickleKey), "/config/crypto.db")
	if err != nil {
		return nil, fmt.Errorf("creating crypto helper: %w", err)
	}

	helper.LoginAs = &mautrix.ReqLogin{
		Type:                     mautrix.AuthTypePassword,
		Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: cfg.Matrix.UserID},
		Password:                 cfg.Matrix.Password,
		InitialDeviceDisplayName: "Recording Bot",
	}

	helper.DecryptErrorCallback = func(evt *event.Event, err error) {
		log.Warn("failed to decrypt event", "room", evt.RoomID, "sender", evt.Sender, "error", err)
	}

	if err := helper.Init(ctx); err != nil {
		return nil, fmt.Errorf("initializing crypto: %w", err)
	}

	return &Client{mx: mx, cryptoHelper: helper, manager: manager, cfg: cfg, log: log}, nil
}

func (c *Client) Close() error {
	return c.cryptoHelper.Close()
}

func (c *Client) Run(ctx context.Context) error {
	syncer := c.mx.Syncer.(*mautrix.DefaultSyncer)

	syncer.OnEvent(func(ctx context.Context, evt *event.Event) {
		c.log.Debug("sync event", "type", evt.Type.Type, "room", evt.RoomID, "sender", evt.Sender)
	})

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
	msg := evt.Content.AsMessage()
	if msg == nil {
		c.log.Warn("nil message content", "room", evt.RoomID, "sender", evt.Sender, "type", evt.Type.Type)
		return
	}
	body := strings.TrimSpace(msg.Body)
	sender := evt.Sender

	c.log.Info("message received", "room", evt.RoomID, "sender", sender, "body", body)

	if sender == id.UserID(c.cfg.Matrix.UserID) {
		return
	}

	switch {
	case strings.HasPrefix(body, "!record-call"):
		c.handleRecordCall(ctx, evt.RoomID, sender, body)
	case body == "!record-stop":
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
		switch err {
		case recorder.ErrAlreadyStopped:
			c.sendText(ctx, roomID, "⏹ Запись уже остановлена автоматически после завершения звонка")
		case recorder.ErrNoRecording:
			c.sendText(ctx, roomID, "Нет активной записи в этой комнате")
		default:
			c.sendText(ctx, roomID, fmt.Sprintf("Ошибка остановки записи: %v", err))
		}
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
	if !c.hasActiveCall(ctx, roomID) {
		return "", fmt.Errorf("no active call found in room")
	}

	livekitRoom, err := c.resolveRoomViaJWTService(ctx, roomID)
	if err != nil {
		return "", fmt.Errorf("resolving livekit room: %w", err)
	}
	return livekitRoom, nil
}

func (c *Client) hasActiveCall(ctx context.Context, roomID id.RoomID) bool {
	stateMap, err := c.mx.State(ctx, roomID)
	if err != nil {
		return false
	}
	for evtType, stateKeys := range stateMap {
		if evtType.Type != "org.matrix.msc3401.call.member" && evtType.Type != "m.call.member" {
			continue
		}
		for _, evt := range stateKeys {
			if evt.Content.Raw == nil {
				continue
			}
			raw, _ := json.Marshal(evt.Content.Raw)
			var content struct {
				FociPreferred []struct {
					Type string `json:"type"`
				} `json:"foci_preferred"`
			}
			if json.Unmarshal(raw, &content) == nil && len(content.FociPreferred) > 0 {
				return true
			}
		}
	}
	return false
}

func (c *Client) resolveRoomViaJWTService(ctx context.Context, roomID id.RoomID) (string, error) {
	openIDToken, err := c.getOpenIDToken(ctx)
	if err != nil {
		return "", fmt.Errorf("getting openid token: %w", err)
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"room":      string(roomID),
		"device_id": "RECORDING_BOT",
		"openid_token": map[string]interface{}{
			"access_token":     openIDToken.AccessToken,
			"token_type":       openIDToken.TokenType,
			"matrix_server_name": openIDToken.MatrixServerName,
			"expires_in":       openIDToken.ExpiresIn,
		},
	})

	jwtURL := c.cfg.LiveKit.JWTServiceURL + "/sfu/get"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, jwtURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling jwt service: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("jwt service returned %d: %s", resp.StatusCode, string(body))
	}

	var sfuResp struct {
		JWT string `json:"jwt"`
	}
	if err := json.Unmarshal(body, &sfuResp); err != nil {
		return "", fmt.Errorf("parsing jwt service response: %w", err)
	}

	return extractRoomFromJWT(sfuResp.JWT)
}

type openIDToken struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	MatrixServerName string `json:"matrix_server_name"`
	ExpiresIn        int    `json:"expires_in"`
}

func (c *Client) getOpenIDToken(ctx context.Context) (*openIDToken, error) {
	url := c.cfg.Matrix.Homeserver + "/_matrix/client/v3/user/" + c.cfg.Matrix.UserID + "/openid/request_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.mx.AccessToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openid request failed %d: %s", resp.StatusCode, string(body))
	}

	var token openIDToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func extractRoomFromJWT(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decoding JWT payload: %w", err)
	}
	var claims struct {
		Video struct {
			Room string `json:"room"`
		} `json:"video"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parsing JWT claims: %w", err)
	}
	if claims.Video.Room == "" {
		return "", fmt.Errorf("no room in JWT claims")
	}
	return claims.Video.Room, nil
}

func (c *Client) sendText(ctx context.Context, roomID id.RoomID, text string) {
	_, err := c.mx.SendText(ctx, roomID, text)
	if err != nil {
		c.log.Error("failed to send message", "room", roomID, "error", err)
	}
}
