# 03 · Configuration

The bot reads two files. **Both are git-ignored and must never be committed.** Copy them from the examples:

```sh
cp .env.example .env
cp projects.example.yaml projects.yaml
```

## `.env`: secrets and server settings

| Variable | Required | What it is |
|---|---|---|
| `DISCORD_TOKEN` | Yes | Bot token from the Discord Developer Portal |
| `GITHUB_TOKEN` | For GitHub sync | Fine-grained token with **read-only** Contents, Metadata, Pull requests and Issues on the project repos. Without it, uploads still work but nothing is mirrored |
| `TAILSCALE_IP` | Yes (Docker) | The server's Tailscale IP. The docsync API binds only to this address |
| `PUBLIC_API_URL` | Recommended | The address teammates use for `docsync login`, shown by `/token` |

These are read by the program itself, and Docker sets them for you:

| Variable | Default | What it is |
|---|---|---|
| `CONFIG_PATH` | `projects.yaml` | Path to the project config |
| `DB_PATH` | `data/hub.db` | SQLite database |
| `API_ADDR` | `:8080` | Where the docsync API listens inside the container |

## `projects.yaml`: server and projects

```yaml
guild_id: "123…"          # the Save Point Discord server
team_role_id: ""          # role allowed to use commands; empty = everyone in the server
max_file_mb: 10           # Discord's upload limit: 10 (no boosts), 50 (level 2), 100 (level 3)
poll_minutes: 3           # how often GitHub is checked

projects:
  - name: memeline                    # lowercase; used in /upload and docsync
    forum_channel_id: "123…"
    repo: LoganShmogan/MemeLine       # optional; leave out for upload-only projects
    branch: main                      # check the repo's default branch (some use master)
    docs: ["docs/**", "README.md"]
    dev_files: ["requirements.txt"]
```

### Choosing files with globs

- `docs/**` matches everything under `docs/` at any depth. `Docs/*.md` matches only the Markdown files directly inside `Docs/`.
- **Globs are case-sensitive.** `docs/**` won't match a `Docs/` folder.
- If a path matches `dev_files` it gets the `dev` tag. Otherwise, if it matches `docs` it gets the `docs` tag. Anything else isn't mirrored.
- Leave out generated copies, such as PDF exports of the same Markdown, so each document has one post.
- Never list a file that contains secrets. Use `.env.example`, never `.env`.

## Adding a project

1. Create its forum channel with the tags (see [02 · Discord setup](02-discord-setup.md)).
2. Add an entry to `projects.yaml`.
3. Run `make secrets`. This copies the config to the server and restarts the bot. On startup it posts every matching file and creates the pinned activity post.
