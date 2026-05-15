package recorder

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

var ErrNoRecording = fmt.Errorf("no active recording in this room")
var ErrAlreadyStopped = fmt.Errorf("recording already stopped")

type Manager struct {
	mu              sync.Mutex
	sessions        map[string]*Session // key: Matrix room ID
	recentlyStopped map[string]time.Time
	lk              *LiveKitClient
	log             *slog.Logger
}

func NewManager(lk *LiveKitClient, log *slog.Logger) *Manager {
	return &Manager{
		sessions:        make(map[string]*Session),
		recentlyStopped: make(map[string]time.Time),
		lk:              lk,
		log:             log,
	}
}

func (m *Manager) StartRecording(ctx context.Context, matrixRoomID, livekitRoom, initiator string, mode Mode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[matrixRoomID]; exists {
		return fmt.Errorf("recording already active in this room")
	}

	egressID, err := m.lk.StartRecording(ctx, livekitRoom, mode)
	if err != nil {
		return err
	}

	m.sessions[matrixRoomID] = &Session{
		RoomID:      matrixRoomID,
		EgressID:    egressID,
		Mode:        mode,
		StartTime:   time.Now(),
		Initiator:   initiator,
		LiveKitRoom: livekitRoom,
	}

	m.log.Info("recording started",
		"room", matrixRoomID,
		"livekit_room", livekitRoom,
		"mode", string(mode),
		"egress_id", egressID,
	)
	return nil
}

func (m *Manager) StopRecording(ctx context.Context, matrixRoomID string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[matrixRoomID]
	if !exists {
		if t, ok := m.recentlyStopped[matrixRoomID]; ok && time.Since(t) < 60*time.Second {
			return nil, ErrAlreadyStopped
		}
		return nil, ErrNoRecording
	}

	if err := m.lk.StopRecording(ctx, session.EgressID); err != nil {
		return nil, err
	}

	delete(m.sessions, matrixRoomID)
	m.recentlyStopped[matrixRoomID] = time.Now()

	m.log.Info("recording stopped",
		"room", matrixRoomID,
		"egress_id", session.EgressID,
		"duration", session.Duration().String(),
	)
	return session, nil
}

func (m *Manager) StopByLiveKitRoom(ctx context.Context, livekitRoom string) (*Session, error) {
	m.mu.Lock()
	var found *Session
	var foundKey string
	for key, s := range m.sessions {
		if s.LiveKitRoom == livekitRoom {
			found = s
			foundKey = key
			break
		}
	}
	m.mu.Unlock()

	if found == nil {
		return nil, nil
	}
	return m.StopRecording(ctx, foundKey)
}

func (m *Manager) HandleEgressEnded(egressID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, s := range m.sessions {
		if s.EgressID == egressID {
			delete(m.sessions, key)
			m.recentlyStopped[key] = time.Now()
			m.log.Info("egress ended externally",
				"room", key,
				"egress_id", egressID,
			)
			return s
		}
	}
	return nil
}

func (m *Manager) GetSession(matrixRoomID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[matrixRoomID]
}

func (m *Manager) RestoreFromEgress(ctx context.Context) error {
	items, err := m.lk.ListActiveEgresses(ctx)
	if err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, info := range items {
		m.sessions["restored:"+info.RoomName] = &Session{
			RoomID:      "restored:" + info.RoomName,
			EgressID:    info.EgressId,
			Mode:        ModeFull,
			StartTime:   time.Unix(info.StartedAt, 0),
			LiveKitRoom: info.RoomName,
		}
		m.log.Info("restored session from egress",
			"egress_id", info.EgressId,
			"room", info.RoomName,
		)
	}
	return nil
}
