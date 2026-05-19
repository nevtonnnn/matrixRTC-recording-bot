# Deployment guide

Step-by-step guide to deploy the recording bot on a server managed by [matrix-docker-ansible-deploy](https://github.com/spantaleev/matrix-docker-ansible-deploy).

## Step 1. Copy the Ansible role

Copy the `ansible/` directory from this repo into your playbook as a custom role:

```bash
cp -r ansible/ /path/to/matrix-docker-ansible-deploy/roles/custom/matrix-recording-bot
```

Resulting structure:

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

## Step 2. Register the role in setup.yml

Open `setup.yml` and add the role **after** the LiveKit roles:

```yaml
    - galaxy/livekit_server
    - custom/matrix-livekit-jwt-service
    - custom/matrix-recording-bot          # <-- add this line
```

## Step 3. Configure variables

Edit `inventory/host_vars/<your-domain>/vars.yml`:

```yaml
# MatrixRTC must be enabled
matrix_rtc_enabled: true

# Valkey (Redis) is required by LiveKit Egress
# If you already have matrix_synapse_workers_enabled: true, Valkey is likely
# already running. Add this line anyway to be explicit:
valkey_enabled: true

# Recording bot
matrix_recording_bot_enabled: true
matrix_recording_bot_matrix_password: "your-bot-password"
matrix_recording_bot_matrix_pickle_key: "random-secret-for-e2ee-storage"

# Video resolution (optional, default 720)
# CPU cost per recording: 360p ~1 core, 480p ~1.5, 720p ~3, 1080p ~4+
# matrix_recording_bot_max_video_height: 1080

# Default recording mode (optional, default "screen")
# matrix_recording_bot_default_mode: "screen"
```

Add LiveKit webhook configuration (so LiveKit notifies the bot about call events) and Valkey connectivity:

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

## Step 4. Build the Docker image

From the bot source directory:

```bash
docker build -t matrix-recording-bot:dev .
```

## Step 5. Load the image on the server

```bash
docker save matrix-recording-bot:dev | ssh your-server "sudo docker load"
```

This pipes the image archive over SSH directly into `docker load` on the server. No need for a Docker registry.

> **Note:** The SSH user must be able to run `sudo docker load` without a password prompt. If sudo asks for a password, configure passwordless sudo first:
> ```bash
> ssh your-server "echo 'your-user ALL=(ALL) NOPASSWD: /usr/bin/docker, /usr/bin/docker *' | sudo tee /etc/sudoers.d/docker-nopasswd && sudo chmod 440 /etc/sudoers.d/docker-nopasswd"
> ```

## Step 6. Create the bot Matrix account

The bot needs a Matrix user account. Register it before the first run:

```bash
just register-user recording-bot "your-bot-password" no
```

The third argument (`no`) means the account is not an admin.

> **Important:** The password here must match `matrix_recording_bot_matrix_password` in your vars.yml.

## Step 7. Deploy with Ansible

Deploy the recording bot role:

```bash
just run-tags install-recording-bot,start
```

Then redeploy LiveKit server to apply the webhook + redis config:

```bash
just run-tags install-livekit-server,start
```

> **Why two commands?** The `install-recording-bot` tag only touches the recording bot role. But we also changed `livekit_server_configuration_extension_yaml` (webhooks + redis), which belongs to the LiveKit server role. You need to redeploy LiveKit server separately.
>
> Alternatively, `just run-tags setup-all,start` deploys everything at once.

## Step 8. Enable and start the services

After Ansible installs the systemd units, they may be `disabled` (not auto-started). Enable and start them:

```bash
ssh your-server "sudo systemctl enable --now matrix-recording-bot matrix-livekit-egress"
```

## Step 9. Verify

Check that both services are running:

```bash
ssh your-server "sudo systemctl status matrix-recording-bot matrix-livekit-egress"
```

Check bot logs:

```bash
ssh your-server "sudo journalctl -u matrix-recording-bot -n 20 --no-pager"
```

You should see:
```
bot started  homeserver=... user=@recording-bot:your-domain output_dir=/recordings
```

Check Egress logs:

```bash
ssh your-server "sudo journalctl -u matrix-livekit-egress -n 20 --no-pager"
```

You should see:
```
connecting to redis  addr=matrix-valkey:6379
service ready
```

## Step 10. Invite the bot

Invite `@recording-bot:your-domain` to any Matrix room. The bot auto-joins on invite. Then start a call and type `!record screen` (or `full` / `voice`).

---

## Troubleshooting

### `M_FORBIDDEN: Invalid username or password`

```
failed to create matrix client  error="initializing crypto: M_FORBIDDEN (HTTP 403): Invalid username or password"
```

**Cause:** The bot's Matrix user account doesn't exist yet, or the password in vars.yml doesn't match.

**Fix:** Register the user (Step 6):
```bash
just register-user recording-bot "your-bot-password" no
```

### `egress not connected (redis required)`

```
failed to restore sessions from egress  error="list egress: twirp error internal: twirp error unknown: egress not connected (redis required)"
```

**Cause:** LiveKit Egress hasn't started yet, or LiveKit server doesn't have Redis configured.

**Fix:**
1. Check that Egress is running: `sudo systemctl status matrix-livekit-egress`
2. If Egress is running but still getting this error, redeploy LiveKit server config:
   ```bash
   just run-tags install-livekit-server,start
   ```
3. Restart LiveKit server: `sudo systemctl restart matrix-livekit-server`
4. Restart the bot: `sudo systemctl restart matrix-recording-bot`

### `no response from servers`

```
start voice egress: twirp error unavailable: twirp error unknown: no response from servers
```

**Cause:** LiveKit Egress is not running or not connected to Valkey.

**Fix:**
1. Check Valkey: `sudo docker ps | grep valkey`
2. Check Egress: `sudo systemctl status matrix-livekit-egress`
3. If Valkey was restarted without Egress, restart Egress: `sudo systemctl restart matrix-livekit-egress`

### `network matrix-valkey not found`

**Cause:** Valkey is not enabled. The Docker network doesn't exist.

**Fix:** Add `valkey_enabled: true` to your vars.yml and run:
```bash
just run-tags setup-all,start
```

### `crypto.db: readonly database`

**Cause:** The bot's crypto database is owned by root instead of the matrix user.

**Fix:**
```bash
ssh your-server "sudo chown matrix:matrix /matrix/recording-bot/config/crypto.db"
sudo systemctl restart matrix-recording-bot
```

### Services installed but not started after Ansible

**Cause:** The recording bot services are not registered in `devture_systemd_service_manager_services_list_auto`, so `just start-all` doesn't manage them.

**Fix:** Enable them manually:
```bash
ssh your-server "sudo systemctl enable --now matrix-recording-bot matrix-livekit-egress"
```

### Silent audio in screen recording mode

**Cause:** The layout page must attach audio tracks as `<audio>` HTML elements for Chrome/PulseAudio to capture them. If audio tracks are not attached, Egress records silence.

**Fix:** This is already fixed in the current version. If you're on an older version, update the bot image.

### Recordings are noise/static in encrypted rooms

**Cause:** LiveKit media E2EE (SFrame) encrypts each sender's media independently. Egress doesn't have decryption keys, so it records encrypted frames as noise.

**Fix:** There is no fix. Recording only works in unencrypted Matrix rooms. Calls started from encrypted rooms use per-participant media encryption which cannot be disabled server-side.

---

## Updating the bot

After making code changes:

```bash
# 1. Rebuild
docker build -t matrix-recording-bot:dev .

# 2. Load on server
docker save matrix-recording-bot:dev | ssh your-server "sudo docker load"

# 3. Restart
ssh your-server "sudo systemctl restart matrix-recording-bot"
```

No need to re-run Ansible unless you changed the Ansible role or vars.yml.
