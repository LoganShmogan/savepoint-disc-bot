# 08 · Development

You need Go 1.27 or later. Docker is optional; you only need it to test the image.

```sh
make test                      # go vet + unit tests
go run ./cmd/bot               # needs .env exported: set -a; . ./.env; set +a
go run ./cmd/docsync help
```

To try changes without touching the real forums, point a copy of `projects.yaml` at a test forum and use a separate database:

```sh
CONFIG_PATH=test-projects.yaml DB_PATH=/tmp/test.db API_ADDR=127.0.0.1:18080 go run ./cmd/bot
```

Don't share a database between a local run and the server. Each one thinks it owns the posts, which leads to duplicates.

## Code layout

```
cmd/bot/            entry point: Discord + GitHub watcher + docsync API
cmd/docsync/        the CLI teammates run
internal/config/    loads projects.yaml, project validation, glob matching (docs vs dev)
internal/store/     SQLite: projects, files, gh_state, cli_tokens
internal/filesync/  Upsert(): the one create / dedupe / replace path
internal/discord/   forum posting (Poster), slash commands (Bot), /project (project.go)
internal/ghwatch/   GitHub polling, mirroring, activity feed
internal/api/       HTTP API for docsync
```

`filesync.Upsert` is the heart of the bot. Every source calls it, so dedupe, versioning and the GitHub ownership rule behave the same everywhere. Change it with care and keep `filesync_test.go` passing.

## Tests

| Package | Covers |
|---|---|
| `filesync` | Create, unchanged, replace, GitHub ownership and `force`, deleted posts recreated, removed-then-restored files, size limit, path cleaning |
| `api` | Token auth, tokens limited to their own server, check, upload, unchanged re-check, size limit, attribution |
| `store` | Project storage per server, moving pre-multi-server rows onto the home server, forgetting a removed server |
| `config` | Glob matching for docs vs dev files |

The Discord and GitHub layers sit behind small interfaces (`filesync.Poster`, `ghwatch.Notifier`), so the logic is tested with fakes and no network.

## Adding a slash command

1. Add it to `commands` in `internal/discord/commands.go`.
2. Handle it in `onInteraction`.
3. Deploy. Commands are registered globally on startup, so they work in every server the bot is in. Restart Discord (Ctrl+R) if a change doesn't show up straight away.

If the change collects any new data, update [07 · Data and security](07-data-and-security.md) and the [Privacy Policy](../legal/privacy-policy.md).

## Ownership of contributions

This is Save Point Limited's proprietary software (see [LICENSE](../LICENSE)). Work contributed to this repository belongs to Save Point Limited.
