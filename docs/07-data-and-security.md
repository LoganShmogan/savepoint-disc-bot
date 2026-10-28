# 07 · Data and security

This doc explains how the bot handles data in practice. The binding commitments are in the [Privacy Policy](../legal/privacy-policy.md), and the two must stay in step: if you change what the bot collects, update the policy, both here and on savepoint.co.nz.

## What the bot stores

All of the bot's own data is in one SQLite database on Save Point's server.

| Table | Contents |
|---|---|
| `files` | Project, path, SHA-256 hash, git blob SHA, version, source (`github` / `upload` / `cli`), Discord post ID, display name of who last changed it, timestamp |
| `gh_state` | Last commit seen and activity post ID per project |
| `cli_tokens` | **SHA-256 hash** of each docsync token, the Discord user ID and display name. The token itself is never stored |

The files themselves are stored by Discord, as attachments, and by GitHub. The bot keeps no copies of their contents.

## What the bot can access

| System | Access | Limits |
|---|---|---|
| Discord | Gateway intent `Guilds` only; the permissions in [02](02-discord-setup.md) | No message content, no member list, no DMs |
| GitHub | Fine-grained token, **read-only**: Contents, Metadata, Pull requests, Issues | Only the repos selected for the token |
| Network | Outbound to Discord and GitHub; inbound on port 8080 **only on the Tailscale IP** | Nothing is exposed to the internet, and there is no dashboard |

## Access control

- **Slash commands:** if `team_role_id` is set, only members with that role can use them. Commands don't work in DMs.
- **docsync API:** every request needs a valid token (`Authorization: Bearer`). Tokens are 192-bit random values, compared by hash. `/token` replaces a user's previous token. Each change is credited to the token's owner.
- **Paths:** uploaded paths are normalised, and anything that tries to escape the project (`../`) is rejected.

## Secrets

- `.env` and `projects.yaml` are git-ignored and must never be committed.
- On the server they live in a folder only the owner can open (mode 700), and `.env` is mode 600. `make secrets` sets these permissions.
- Nothing in the bot logs tokens.
- If a token leaks, see [SECURITY.md](../SECURITY.md).

## Logs

Docker keeps at most 3 × 10 MB of logs, and older entries are deleted automatically. Logs contain project names, file paths, sync results and errors, but no file contents or tokens.

## Deleting someone's data

If a person asks for their data to be removed (Privacy Policy section 2.5):

1. Remove their docsync token by deleting their row from `cli_tokens`.
2. Their display name appears in `files.updated_by` and in the bot's notes in Discord. Replace or remove it as requested.
3. Respond within 20 working days.
