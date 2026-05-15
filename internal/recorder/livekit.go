package recorder

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

type LiveKitClient struct {
	egress         *lksdk.EgressClient
	outputDir      string
	layoutURL      string
	maxVideoHeight int32
}

func NewLiveKitClient(url, apiKey, apiSecret, outputDir, layoutURL string, maxVideoHeight int32) *LiveKitClient {
	return &LiveKitClient{
		egress:         lksdk.NewEgressClient(url, apiKey, apiSecret),
		outputDir:      outputDir,
		layoutURL:      layoutURL,
		maxVideoHeight: maxVideoHeight,
	}
}

func (c *LiveKitClient) videoEncoding() *livekit.EncodingOptions {
	h := c.maxVideoHeight
	w := h * 16 / 9
	return &livekit.EncodingOptions{
		Width:   w,
		Height:  h,
		Depth:   24,
		Framerate: 30,
		VideoCodec: livekit.VideoCodec_H264_BASELINE,
	}
}

func (c *LiveKitClient) outputPath(roomName string, mode Mode) string {
	ts := time.Now().Format("2006-01-02_15-04")
	ext := "mp4"
	if mode == ModeVoice {
		ext = "ogg"
	}
	safeName := strings.ReplaceAll(roomName, "/", "_")
	return filepath.Join(c.outputDir, safeName, fmt.Sprintf("%s.%s", ts, ext))
}

func (c *LiveKitClient) StartRecording(ctx context.Context, roomName string, mode Mode) (string, error) {
	switch mode {
	case ModeFull:
		return c.startFull(ctx, roomName)
	case ModeScreen:
		return c.startScreen(ctx, roomName)
	case ModeVoice:
		return c.startVoice(ctx, roomName)
	default:
		return "", fmt.Errorf("unknown mode: %s", mode)
	}
}

func (c *LiveKitClient) startFull(ctx context.Context, roomName string) (string, error) {
	out := c.outputPath(roomName, ModeFull)
	info, err := c.egress.StartRoomCompositeEgress(ctx, &livekit.RoomCompositeEgressRequest{
		RoomName: roomName,
		Layout:   "grid",
		Options:  &livekit.RoomCompositeEgressRequest_Advanced{Advanced: c.videoEncoding()},
		FileOutputs: []*livekit.EncodedFileOutput{
			{
				FileType: livekit.EncodedFileType_MP4,
				Filepath: out,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("start full egress: %w", err)
	}
	return info.EgressId, nil
}

func (c *LiveKitClient) startScreen(ctx context.Context, roomName string) (string, error) {
	out := c.outputPath(roomName, ModeScreen)
	info, err := c.egress.StartRoomCompositeEgress(ctx, &livekit.RoomCompositeEgressRequest{
		RoomName:      roomName,
		CustomBaseUrl: c.layoutURL,
		Options:       &livekit.RoomCompositeEgressRequest_Advanced{Advanced: c.videoEncoding()},
		FileOutputs: []*livekit.EncodedFileOutput{
			{
				FileType: livekit.EncodedFileType_MP4,
				Filepath: out,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("start screen egress: %w", err)
	}
	return info.EgressId, nil
}

func (c *LiveKitClient) startVoice(ctx context.Context, roomName string) (string, error) {
	out := c.outputPath(roomName, ModeVoice)
	info, err := c.egress.StartRoomCompositeEgress(ctx, &livekit.RoomCompositeEgressRequest{
		RoomName:  roomName,
		AudioOnly: true,
		FileOutputs: []*livekit.EncodedFileOutput{
			{
				FileType: livekit.EncodedFileType_OGG,
				Filepath: out,
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("start voice egress: %w", err)
	}
	return info.EgressId, nil
}

func (c *LiveKitClient) StopRecording(ctx context.Context, egressID string) error {
	_, err := c.egress.StopEgress(ctx, &livekit.StopEgressRequest{
		EgressId: egressID,
	})
	if err != nil {
		return fmt.Errorf("stop egress: %w", err)
	}
	return nil
}

func (c *LiveKitClient) ListActiveEgresses(ctx context.Context) ([]*livekit.EgressInfo, error) {
	resp, err := c.egress.ListEgress(ctx, &livekit.ListEgressRequest{
		Active: true,
	})
	if err != nil {
		return nil, fmt.Errorf("list egress: %w", err)
	}
	return resp.Items, nil
}
