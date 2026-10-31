# 01 · Overview

Save Point Project Hub is Save Point Limited's internal Discord bot. It turns our Discord server into one place for every project. Each project has a forum channel holding its docs, dev files, GitHub activity and team discussion. The code stays on GitHub.

## What lives where

```
Discord server
└── PROJECTS (category)
    ├── # memeline        (forum channel, one per project)
    │   ├── 📌 📡 GitHub Activity     [github]  commits, PRs, issues, releases
    │   ├── docs/architecture.md      [docs]    one post per file, updated in place
    │   ├── README.md                 [docs]
    │   ├── requirements.txt          [dev]
    │   └── 💬 Discussion posts        made by the team
    ├── # jampacked
    └── …
```

## How it works

```
 GitHub repos ──(poll every 3 min)──┐
                                    ▼
 /upload in Discord ─────────▶  filesync  ──▶  Discord forum posts
                                    ▲          (create / replace / archive)
 docsync CLI ──(Tailscale only)─────┘
                                    │
                               SQLite store
                  (hashes, versions, post IDs, hashed tokens)
```

Every file, wherever it comes from, goes through the same step, **filesync**:

1. Hash the file (SHA-256).
2. If it has never been posted, create a forum post with the file attached and tag it `docs` or `dev`.
3. If it was posted before and the hash matches, do nothing ("already up to date").
4. If the hash differs, replace the attachment in the existing post, bump the version (v1 → v2) and add a short note saying who changed it and how.
5. If the file came from GitHub, uploads can't overwrite it unless forced. GitHub stays the source of truth.

## What it doesn't do

- **No dashboard and no website.** You use it only through Discord slash commands and the `docsync` CLI.
- **It doesn't read chat.** The bot doesn't request Discord's message content access and ignores conversations.
- **It doesn't write to GitHub.** Its GitHub token is read-only.
- **It isn't public.** It runs only in Discord servers Save Point runs. Each server has its own projects, made with `/project create`.

## Built with

| Part | Technology |
|---|---|
| Language | Go |
| Discord | [discordgo](https://github.com/bwmarrin/discordgo) (gateway with the Guilds intent only) |
| GitHub | [go-github](https://github.com/google/go-github), polling with a read-only fine-grained token |
| Storage | SQLite via [modernc.org/sqlite](https://modernc.org/sqlite), pure Go with no C compiler needed |
| Hosting | Docker Compose on Save Point's server, private access via Tailscale |
