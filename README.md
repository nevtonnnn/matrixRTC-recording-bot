# matrixRTC-recording-bot

Bot for recording MatrixRTC (Element Call / LiveKit) calls from Matrix rooms.

Listens for commands in Matrix rooms, starts [LiveKit Egress](https://docs.livekit.io/egress-ingress/) recordings, and saves files to disk.

## Features

- **Three recording modes:**
  - `full` — grid view of all participants (video + audio, MP4)
  - `screen` — screen share capture with custom layout (video + audio, MP4)
  - `voice` — audio only (OGG)
- Automatic stop when the call ends (via LiveKit webhooks)
- E2EE support — the bot can join encrypted Matrix rooms and read commands
- Session restore on restart — picks up in-progress egress sessions
- **Nextcloud integration** — upload recordings to Nextcloud and share download links in chat

## Architecture

```
Matrix Room ──> Recording Bot ──> LiveKit Egress ──> MP4/OGG files
                     │                   │
                     │ webhooks           │ RoomComposite
                     │                   │
              LiveKit Server ◄───────────┘
                     │
                   Valkey (Redis)
```

The bot itself does not process media. It acts as a control plane:
1. Receives `!record <mode>` command from a Matrix room
2. Resolves the LiveKit room name via the JWT service
3. Starts a LiveKit Egress (RoomComposite) recording
4. Listens for LiveKit webhooks to detect call/egress completion

LiveKit Egress runs Chrome headlessly, renders a layout page, and captures audio/video via PulseAudio + screen capture.

## Commands

| Command | Description |
|---------|-------------|
| `!record full` | Record grid view of all participants (video + audio) |
| `!record screen` | Record screen share (video + audio) |
| `!record voice` | Record audio only |
| `!record stop` | Stop recording |

## Quick start

See [how-to-deploy.md](how-to-deploy.md) for a detailed step-by-step deployment guide with troubleshooting.

## Prerequisites

- A server deployed with [matrix-docker-ansible-deploy](https://github.com/spantaleev/matrix-docker-ansible-deploy)
- MatrixRTC enabled (`matrix_rtc_enabled: true`)
- Valkey enabled (`valkey_enabled: true`) — required by LiveKit Egress for communication with LiveKit server
- A Matrix account for the bot

## Configuration reference

All Ansible variables with defaults:

| Variable | Default | Description |
|----------|---------|-------------|
| `matrix_recording_bot_enabled` | `false` | Enable/disable the bot |
| `matrix_recording_bot_matrix_password` | `""` | **Required.** Bot account password |
| `matrix_recording_bot_matrix_pickle_key` | `"change-me..."` | Secret for E2EE database encryption |
| `matrix_recording_bot_default_mode` | `"screen"` | Default recording mode |
| `matrix_recording_bot_max_video_height` | `720` | Video resolution (CPU: 360p~1 core, 720p~3, 1080p~4+) |
| `matrix_recording_bot_max_concurrent` | `1` | Max simultaneous recordings (0 = unlimited) |
| `matrix_recording_bot_egress_enabled` | same as bot | Deploy LiveKit Egress alongside the bot |
| `matrix_recording_bot_egress_version` | `"v1.9.1"` | LiveKit Egress Docker image version |
| `matrix_recording_bot_nextcloud_enabled` | `false` | Enable Nextcloud upload integration |
| `matrix_recording_bot_nextcloud_url` | `""` | Nextcloud server URL (e.g. `https://cloud.example.com`) |
| `matrix_recording_bot_nextcloud_username` | `""` | Nextcloud username for the bot |
| `matrix_recording_bot_nextcloud_password` | `""` | Nextcloud app password |
| `matrix_recording_bot_nextcloud_upload_dir` | `"/Recordings"` | Upload directory on Nextcloud |
| `matrix_recording_bot_nextcloud_delete_after_upload` | `false` | Delete local file after successful upload |
| `matrix_recording_bot_nextcloud_retention_days` | `30` | Auto-delete recordings after N days (0 = disabled) |

## Recordings

Files are saved to `/matrix/recordings/<livekit-room-name>/` on the server:
- `2026-01-15_14-30.mp4` — full/screen modes
- `2026-01-15_14-30.ogg` — voice mode

## Limitations

- **No E2EE call recording.** LiveKit media E2EE (SFrame) encrypts each sender's media independently. Egress cannot decrypt these streams. Recording only works for calls without per-participant media encryption (i.e., calls initiated from unencrypted Matrix rooms).
- One recording per Matrix room at a time.
- The bot needs network access to LiveKit server, JWT service, Synapse, and Valkey.

## Local development

```bash
cp config.example.yaml config.yaml
# Edit config.yaml with your server details

CGO_ENABLED=1 go build -tags goolm -o bot ./cmd/bot/
./bot -config config.yaml
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
