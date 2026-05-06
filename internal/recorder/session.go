package recorder

import "time"

type Mode string

const (
	ModeFull   Mode = "full"
	ModeScreen Mode = "screen"
	ModeVoice  Mode = "voice"
)

func ParseMode(s string, defaultMode string) (Mode, bool) {
	switch s {
	case "full":
		return ModeFull, true
	case "screen":
		return ModeScreen, true
	case "voice":
		return ModeVoice, true
	case "":
		return ParseMode(defaultMode, "screen")
	default:
		return "", false
	}
}

type Session struct {
	RoomID      string
	EgressID    string
	Mode        Mode
	StartTime   time.Time
	Initiator   string
	LiveKitRoom string
}

func (s *Session) FileExtension() string {
	if s.Mode == ModeVoice {
		return "ogg"
	}
	return "mp4"
}

func (s *Session) Duration() time.Duration {
	return time.Since(s.StartTime)
}
