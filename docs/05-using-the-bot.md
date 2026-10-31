# 05 · Using the bot

This guide is for everyone on the Save Point team.

## Slash commands

Type `/` in any channel of a server the bot is in. Replies are private to you. Each server has its own projects.

### `/project create name [repo] [branch] [docs] [dev_files] [category] [forum]`

Makes a forum for a project and starts syncing it. You need **Manage Channels** to use it.

- `name`: lowercase, no spaces, e.g. `memeline`. It's used in `/upload` and docsync.
- `repo`: optional GitHub repo to mirror, as `owner/name`. Leave it out for an upload-only project. Servers other than Save Point's can only use public repos.
- `branch`: defaults to `main`. Check the repo's default branch, because some use `master`.
- `docs` / `dev_files`: comma-separated globs, e.g. `docs/**, README.md`. `docs` defaults to `docs/**, README.md` when a repo is set. See [globs](03-configuration.md#choosing-files-with-globs).
- `category`: where to put the new forum.
- `forum`: use an existing forum instead of making a new one. Missing tags are added to it.

### `/project edit project [repo] [branch] [docs] [dev_files]`

Changes the options you give and leaves the rest alone. Use `-` to clear `repo` or `dev_files`.

### `/project remove project`

Stops syncing a project. The forum and its posts are kept.

### `/upload project file [path] [force]`

Adds a file to a project's forum, or replaces an older version of it.

- `project`: start typing and pick from the list.
- `file`: attach the file. The maximum size is shown in `projects.yaml` (10 MB by default).
- `path`: optional. Where the file belongs, e.g. `docs/setup.md`. It defaults to the file's name. If it ends with `/`, the file name is added to the end.
- `force`: overwrite a file that is mirrored from GitHub. Use it rarely, because the next commit to that file will replace your upload again.

| Reply | Meaning |
|---|---|
| ✅ Added | New post created |
| 🔄 Replaced with v3 | The existing post now has your version, with a note saying who changed it |
| 👍 Already up to date | Identical to what's there, so nothing changed |
| 🔒 Managed by GitHub | Push your change to the repo instead, or use `force` |

### `/docs project`

Lists every file the bot manages for a project, with its version, where it came from, when it last changed, and a link. Files removed from the repo are shown struck through.

### `/sync [project]`

Checks GitHub right away instead of waiting for the next poll (every 3 minutes). Leave `project` out to check every project in this server.

### `/token`

Gives you a personal token for the `docsync` CLI, for the projects in the server where you ran it. It is shown once, and asking again (in any server) replaces your old token.

## The `docsync` CLI

Use it to push a whole folder of docs from your computer. Only new or changed files are uploaded.

### Setup (once)

1. Ask Logan for the `docsync` binary for your system (built with `make cli`).
2. Join Save Point's Tailscale network. The CLI can't reach the bot without it.
3. Run `/token` in Discord and copy the `docsync login …` line it gives you:

```sh
docsync login --server http://SERVER:8080 --token phb_…
```

Your token is saved in your user config folder (`~/.config/docsync` on Linux), readable only by you.

### Pushing files

```sh
docsync projects                                        # list project names
docsync push ./docs --project memeline --prefix docs --dry-run   # preview
docsync push ./docs --project memeline --prefix docs             # upload
```

- `--prefix docs` stores `./docs/setup.md` as `docs/setup.md`, matching the repo layout.
- `--dry-run` shows what would happen and uploads nothing.
- `--force` overwrites GitHub-managed files.
- Dotfiles, `node_modules` and `vendor` are skipped.

The output uses `+` for new files, `~` for updated, `=` for unchanged and 🔒 for files managed by GitHub. A summary line follows.
