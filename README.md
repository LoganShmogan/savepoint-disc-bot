<p align="center">
  <a href="https://savepoint.co.nz"><img src="docs/assets/save-point-banner.png" alt="Save Point" width="480"></a>
</p>

<h1 align="center">Save Point Project Hub</h1>

<p align="center">
  The Discord bot that keeps Save Point's project forums in sync with GitHub and the team's own files.<br>
  Built, owned and run by <a href="https://savepoint.co.nz"><b>Save Point Limited</b></a>, New Zealand.
</p>

<p align="center">
  <a href="docs/README.md">Docs</a> ·
  <a href="legal/terms-of-service.md">Terms of Service</a> ·
  <a href="legal/privacy-policy.md">Privacy Policy</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="LICENSE">Licence</a>
</p>

---

> [!IMPORTANT]
> This is Save Point's internal tool and proprietary software. The code is public so clients and partners can see how it works. It is **not** open source, and it is not offered as a public bot. See [LICENSE](LICENSE).

## What it does

Every Save Point project gets its own **forum channel** in our Discord server. The bot keeps each forum up to date:

- **📄 Docs and dev files from GitHub.** Each matching file in the project's repo, such as `docs/**`, `README.md` or `.env.example`, becomes its own forum post. When the file changes, the bot swaps in the new version and notes "🔄 Updated to v3". If a file is deleted from the repo, its post is archived.
- **📡 GitHub activity.** A pinned post in each forum lists new commits, pull requests, issues and releases.
- **⬆️ Uploads with no duplicates.** Team members can add files with `/upload` in Discord or the `docsync` command-line tool. Identical files are skipped, and changed files replace the old version in the same post.
- **🔒 GitHub stays the source of truth.** Files mirrored from a repo can't be overwritten by an upload unless you force it.

### No dashboard

There is **no web dashboard and no public website** for this bot. You use it entirely through Discord's slash commands and the `docsync` tool. Its only network service is a small API for `docsync`, which listens on Save Point's private Tailscale network and is never exposed to the internet.

## Commands

| Command | What it does |
|---|---|
| `/upload project file [path] [force]` | Add or replace a file. Replies *created*, *updated to vN* or *already up to date*. |
| `/docs project` | List a project's files with versions and links. |
| `/sync [project]` | Check GitHub now instead of waiting for the next poll. |
| `/token` | Get a personal token for the `docsync` CLI. |

## Quick start (Save Point staff)

```sh
cp .env.example .env                    # bot + GitHub tokens, Tailscale IP
cp projects.example.yaml projects.yaml  # server, forums and repos
make test
make secrets                            # copy .env and projects.yaml to the server
make deploy                             # copy the code, rebuild, restart
```

The full walkthrough is in **[docs/](docs/README.md)**: Discord setup, forum layout, configuration, deployment, the CLI, data handling and troubleshooting.

## Legal

- [Terms of Service](legal/terms-of-service.md), published at <https://savepoint.co.nz/legal/discord-bot#terms>
- [Privacy Policy](legal/privacy-policy.md), published at <https://savepoint.co.nz/legal/discord-bot#privacy>

## Ownership

**Save Point Project Hub** is © 2026 Save Point Limited, a New Zealand company (company number 9392145). All rights reserved.

- Website: [savepoint.co.nz](https://savepoint.co.nz)
- General enquiries: [contact@savepoint.co.nz](mailto:contact@savepoint.co.nz)
- Support, privacy and security: [support@savepoint.co.nz](mailto:support@savepoint.co.nz)

"Save Point" and the Save Point flag logo are the property of Save Point Limited. Discord and GitHub are trademarks of their respective owners. This project is not affiliated with or endorsed by either.
