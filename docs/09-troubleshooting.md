# 09 · Troubleshooting

Start with the logs:

```sh
ssh "$DEPLOY_HOST" 'cd project-hub-bot && docker compose logs --tail 100 bot'
```

| Symptom | Likely cause | Fix |
|---|---|---|
| Container won't start: `DISCORD_TOKEN is not set` | `.env` missing on the server | `make secrets` |
| Container won't start: `permission denied` reading `projects.yaml` | File isn't readable by the container user | `chmod 644 projects.yaml` on the server (`make secrets` does this) |
| Container won't start: `cannot assign requested address` | Docker started before Tailscale had its IP | `docker compose up -d` |
| `required variable TAILSCALE_IP is missing` | Not set in `.env` | Add it, then `make secrets` |
| Slash commands don't appear | Bot invited without the `applications.commands` scope, or wrong `guild_id` | Re-invite with the link in [02](02-discord-setup.md), and check the ID |
| "You need the team role" | `team_role_id` is set and you don't have that role | Ask an admin for the role |
| Files from a repo never appear | Glob doesn't match (case matters: `Docs/` vs `docs/`), wrong `branch`, or no repo access for the token | Check `projects.yaml`, the default branch, and the token's repo list. Look for `github poll failed` in the logs |
| `get branch main: 404` | The repo's default branch is `master`, or the token can't see the repo | Fix `branch`, or add the repo to the token |
| Posts have no tags | The forum has no `docs` / `dev` / `github` tags | Add them in Edit Channel → Tags. New posts pick them up |
| Activity post isn't pinned | Bot lacks Manage Threads in that forum | Grant it, then pin the post by hand once |
| Every file was posted twice | The bot ran against a fresh database, e.g. a lost volume or a local test run | Delete the duplicates, then restore a backup ([04](04-deployment.md#backups)) |
| `docsync`: `unknown token` | Token replaced by a newer `/token` | Run `/token` again and `docsync login` |
| `docsync`: connection refused or timeout | Not on Tailscale, or wrong server address | Connect to Tailscale; check the URL from `/token` |
| Upload: "over the 10 MB limit" | Discord's limit for the server's boost level | Commit the file to GitHub and link it instead, or raise `max_file_mb` if the server is boosted |

Still stuck? Email support@savepoint.co.nz.
