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
1. Receives `!record-call` command from a Matrix room
2. Resolves the LiveKit room name via the JWT service
3. Starts a LiveKit Egress (RoomComposite) recording
4. Listens for LiveKit webhooks to detect call/egress completion

LiveKit Egress runs Chrome headlessly, renders a layout page, and captures audio/video via PulseAudio + screen capture.

## Commands

| Command | Description |
|---------|-------------|
| `!record-call` | Start recording (uses default mode from config) |
| `!record-call full` | Record grid view of all participants |
| `!record-call screen` | Record screen share |
| `!record-call voice` | Record audio only |
| `!record-stop` | Stop recording |

## Prerequisites

- A server deployed with [matrix-docker-ansible-deploy](https://github.com/spantaleev/matrix-docker-ansible-deploy)
- MatrixRTC enabled (`matrix_rtc_enabled: true`) — this sets up LiveKit server, JWT service, and Element Call
- Valkey enabled (`valkey_enabled: true`) — required by LiveKit Egress
- A Matrix account for the bot (create it manually or let the bot auto-register)

## Deployment with matrix-docker-ansible-deploy

### 1. Copy the Ansible role

Copy the `roles/custom/matrix-recording-bot` directory into your playbook:

```
matrix-docker-ansible-deploy/
  roles/
    custom/
      matrix-recording-bot/
        defaults/main.yml
        tasks/
          main.yml
          setup_install.yml
          setup_uninstall.yml
          validate_config.yml
        templates/
          config.yaml.j2
          egress-config.yaml.j2
          egress-systemd.service.j2
          labels.j2
          systemd.service.j2
```

The full Ansible role is available in the [`ansible/`](ansible/) directory of this repository.

### 2. Register the role in the playbook

Add to `setup.yml` (or your custom playbook file), after the LiveKit roles:

```yaml
- role: custom/matrix-recording-bot
```

Add the recording bot services to your `group_vars/matrix_servers` start/stop lists:

```yaml
# In the start section (devture_systemd_service_manager_services_list_auto)
matrix_recording_bot_services:
  - matrix-recording-bot
  - matrix-livekit-egress

# Or add them to devture_systemd_service_manager_services_list_additional
devture_systemd_service_manager_services_list_additional:
  - matrix-recording-bot
  - matrix-livekit-egress
```

### 3. Build and load the Docker image

The bot is not published to a registry, so you need to build it and load it on the server:

```bash
# Build the image
docker build -t matrix-recording-bot:dev .

# Save and load on the server
docker save matrix-recording-bot:dev | ssh your-server "sudo docker load"
```

### 4. Configure variables

Add to your `inventory/host_vars/<your-domain>/vars.yml`:

```yaml
# Enable MatrixRTC (if not already)
matrix_rtc_enabled: true
valkey_enabled: true

# Recording bot
matrix_recording_bot_enabled: true
matrix_recording_bot_matrix_password: "your-bot-password"
matrix_recording_bot_matrix_pickle_key: "random-secret-for-e2ee-storage"

# Optional: video resolution (default 720)
# CPU cost: 360p ~1 core, 480p ~1.5, 720p ~3, 1080p ~4+
# matrix_recording_bot_max_video_height: 720

# Optional: default recording mode (default "screen")
# matrix_recording_bot_default_mode: "screen"
```

Configure LiveKit to send webhooks to the bot and connect to Valkey:

```yaml
livekit_server_configuration_extension_yaml: |
  webhook:
    api_key: "{{ matrix_livekit_jwt_service_environment_variable_livekit_key }}"
    urls:
      - "http://matrix-recording-bot:8080/webhook"
  redis:
    address: "matrix-valkey:6379"

livekit_server_container_additional_networks_custom:
  - matrix-valkey
```

### 5. Deploy

```bash
# Full setup
just run-tags setup-all,start

# Or just the recording bot
just run-tags install-recording-bot,start
```

### 6. Create the bot account and invite it

Create the bot user via Synapse admin API or synapse-admin (ketesa). Then invite `@recording-bot:your-domain` to any room — the bot auto-joins on invite.

## Configuration reference

All Ansible variables with defaults:

| Variable | Default | Description |
|----------|---------|-------------|
| `matrix_recording_bot_enabled` | `false` | Enable/disable the bot |
| `matrix_recording_bot_matrix_password` | `""` | **Required.** Bot account password |
| `matrix_recording_bot_matrix_pickle_key` | `"change-me..."` | Secret for E2EE database encryption |
| `matrix_recording_bot_default_mode` | `"screen"` | Default recording mode |
| `matrix_recording_bot_max_video_height` | `720` | Video resolution height |
| `matrix_recording_bot_egress_enabled` | same as bot | Deploy LiveKit Egress alongside the bot |
| `matrix_recording_bot_egress_version` | `"v1.9.1"` | LiveKit Egress Docker image version |

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
