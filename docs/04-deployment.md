# 04 · Deployment

The bot runs as a single Docker container on Save Point's server. You manage it over SSH through Tailscale. Server addresses and access details are kept in Save Point's internal infrastructure register, not in this public repo.

## Requirements on the server

- Docker with the Compose plugin
- Tailscale, connected to the Save Point tailnet
- `rsync` and SSH key access from the machine you deploy from

## Make targets

| Command | What it does |
|---|---|
| `make secrets` | Copies `.env` (mode 600) and `projects.yaml` into a private folder on the server, then restarts the bot |
| `make deploy` | Copies the code (never the secrets), rebuilds the image and restarts |
| `make test` | Vet and unit tests |
| `make cli` | Builds `docsync` for Linux, Windows and macOS into `dist/` |

Both deploy targets use `DEPLOY_HOST` (the server's SSH name) and `DEPLOY_DIR` (the folder in its home directory). Override them when needed: `make deploy DEPLOY_HOST=other-server`.

## First deploy

```sh
make secrets   # creates the folder and sends the config
make deploy    # builds and starts the container
ssh "$DEPLOY_HOST" 'cd project-hub-bot && docker compose logs -f'
```

You should see `connected to Discord` and then one `mirrored` line per file. Discord rate-limits new posts, so a first sync of around 80 files takes a few minutes.

## How the container is set up

- **Image:** multi-stage build into `distroless/static` running as a non-root user. It's about 15 MB and has no shell.
- **Restart policy:** `unless-stopped`, so it comes back after crashes and reboots.
- **Data:** the Docker volume `hub-data`, mounted at `/data` (SQLite).
- **Config:** `projects.yaml` mounted read-only. The container user must be able to read it, so it is mode 644 inside the private (700) folder.
- **Logs:** Docker `json-file` with 3 × 10 MB rotation. The Privacy Policy promises size-limited logs, so keep this.
- **Network:** port 8080 is bound to `TAILSCALE_IP` only. If Docker starts before Tailscale has its IP after a reboot, the container fails with "cannot assign requested address". Run `docker compose up -d` again.

## Updating

- **Code change:** `make deploy`
- **Config or token change:** edit locally, then `make secrets`

## Backups

The only state is the SQLite database. Losing it doesn't lose any files, because they're still in Discord and GitHub. But the bot would then post every file again as a duplicate. To back it up:

```sh
ssh "$DEPLOY_HOST" 'docker run --rm -v project-hub-bot_hub-data:/data -v $PWD:/out busybox cp /data/hub.db /out/hub-backup.db'
```

To restore, stop the bot, copy the file back into the volume the same way, and `chown 65532:65532` it.
