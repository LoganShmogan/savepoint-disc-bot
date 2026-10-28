# 06 · GitHub sync

When a `GITHUB_TOKEN` is set, the bot polls every project that has a `repo` in `projects.yaml`, every `poll_minutes` (default 3), or right away with `/sync`.

## Mirroring files

On each poll the bot:

1. Reads the head commit of the project's `branch`.
2. If the head hasn't changed since the last poll, it skips mirroring.
3. Otherwise it lists the repo tree and keeps the files that match `docs` or `dev_files`.
4. For each match, it compares the **git blob SHA** with what it posted last time. It downloads and syncs only the files that changed, so polling stays cheap.
5. Any file it mirrored before that is no longer in the repo gets a "🗑️ Removed from the repo" note, and its post is **archived**. If the file comes back, the same post is revived.

Files over the size limit are skipped with a warning in the logs. Link to them from a doc instead.

## Who owns a file

| Last written by | Can GitHub overwrite it? | Can `/upload` or `docsync` overwrite it? |
|---|---|---|
| GitHub | Yes | Only with `force` |
| `/upload` or `docsync` | Yes, and GitHub takes ownership | Yes |

This keeps the repo as the single source of truth for anything that lives in it.

## Activity feed

Each forum has one pinned **📡 GitHub Activity** post. Each poll adds embeds for:

| Event | Example |
|---|---|
| Commits | 🔨 3 new commits to main, listing up to 10 with links |
| Pull requests | 🔀 PR opened, ✅ PR merged, ❌ PR closed |
| Issues | 🐛 Issue opened, ✔️ Issue closed |
| Releases | 🚀 Release v1.2.0, with release notes |

On a project's first poll, the bot creates the activity post but doesn't replay old history. If someone deletes the activity post, the bot makes a new one.

## Rate limits

A poll with no changes uses 3 GitHub API calls per project (branch, issues, releases). That is about 360 calls an hour for 6 projects, well under GitHub's limit of 5,000 an hour.
