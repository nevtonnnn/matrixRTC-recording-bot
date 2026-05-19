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
	stoppedSessions map[string]*Session // key: egress ID — awaiting egress_ended webhook
	recentlyStopped map[string]time.Time
	lk              *LiveKitClient
	maxConcurrent   int
	log             *slog.Logger
}

func NewManager(lk *LiveKitClient, maxConcurrent int, log *slog.Logger) *Manager {
	return &Manager{
		sessions:        make(map[string]*Session),
		stoppedSessions: make(map[string]*Session),
		recentlyStopped: make(map[string]time.Time),
		lk:              lk,
		maxConcurrent:   maxConcurrent,
		log:             log,
	}
}

const (
	recentStoppedTTL  = 60 * time.Second
	stoppedSessionTTL = 10 * time.Minute
)

func (m *Manager) cleanupStaleLocked() {
	now := time.Now()
	for k, t := range m.recentlyStopped {
		if now.Sub(t) > recentStoppedTTL {
			delete(m.recentlyStopped, k)
		}
	}
	for eid, s := range m.stoppedSessions {
		if !s.StopTime.IsZero() && now.Sub(s.StopTime) > stoppedSessionTTL {
			m.log.Warn("purging stale stopped session (webhook never arrived)",
				"egress_id", eid, "room", s.RoomID)
			delete(m.stoppedSessions, eid)
		}
	}
}

func (m *Manager) StartRecording(ctx context.Context, matrixRoomID, livekitRoom, initiator string, mode Mode) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sessions[matrixRoomID]; exists {
		return fmt.Errorf("recording already active in this room")
	}

	if m.maxConcurrent > 0 && len(m.sessions) >= m.maxConcurrent {
		return fmt.Errorf("достигнут лимит одновременных записей (%d)", m.maxConcurrent)
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

	m.cleanupStaleLocked()

	session, exists := m.sessions[matrixRoomID]
	if !exists {
		if _, ok := m.recentlyStopped[matrixRoomID]; ok {
			return nil, ErrAlreadyStopped
		}
		return nil, ErrNoRecording
	}

	if err := m.lk.StopRecording(ctx, session.EgressID); err != nil {
		return nil, err
	}

	session.StopTime = time.Now()
	delete(m.sessions, matrixRoomID)
	m.stoppedSessions[session.EgressID] = session
	m.recentlyStopped[matrixRoomID] = session.StopTime

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

func (m *Manager) HandleEgressEnded(egressID string) (session *Session, alreadyStopped bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cleanupStaleLocked()

	if s, ok := m.stoppedSessions[egressID]; ok {
		delete(m.stoppedSessions, egressID)
		m.log.Info("egress ended after manual stop",
			"room", s.RoomID,
			"egress_id", egressID,
		)
		return s, true
	}

	for key, s := range m.sessions {
		if s.EgressID == egressID {
			s.StopTime = time.Now()
			delete(m.sessions, key)
			m.recentlyStopped[key] = s.StopTime
			m.log.Info("egress ended externally",
				"room", key,
				"egress_id", egressID,
			)
			return s, false
		}
	}
	return nil, false
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
