# 02 · Discord setup

This is a one-time setup for the Discord application and the project forums.

## 1. Create the application

1. Go to <https://discord.com/developers/applications> → **New Application**. Name it after Save Point (ours is "SavePoint") and upload the Save Point logo as its icon.
2. Under **General Information**, set:
   - **Terms of Service URL:** `https://savepoint.co.nz/legal/discord-bot#terms`
   - **Privacy Policy URL:** `https://savepoint.co.nz/legal/discord-bot#privacy`
3. Under **Bot**:
   - **Reset Token** and put the token in `.env` as `DISCORD_TOKEN`. Never paste it in chat or commit it.
   - **Public Bot** controls who can add the bot to a server. Leave it off and only the application's owner can invite it (to any server they manage). Turn it on only if other people should be able to add it to their own servers. See [Other servers](#6-other-servers) for what they can and can't do.
   - Leave all **Privileged Gateway Intents** off. The bot doesn't need them.

## 2. Invite it

Replace `APPLICATION_ID` with the ID from **General Information**, then open the link and pick a server. Use the same link for every server you add it to:

```
https://discord.com/oauth2/authorize?client_id=APPLICATION_ID&scope=bot+applications.commands&permissions=326417632272
```

`326417632272` grants exactly these permissions: Manage Channels (so `/project create` can make forums), View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Manage Threads, Embed Links, Attach Files and Read Message History.

## 3. Turn on Community

Discord only offers forum channels in **Community** servers:
**Server Settings → Enable Community → Get Started**. It asks for a rules channel and an updates channel. Community doesn't make the server public, so leave **Server Discovery** off.

## 4. Create the project forums

The quickest way is `/project create` (see [05 · Using the bot](05-using-the-bot.md#project-create-name-repo-branch-docs-dev_files-category-forum)). It makes the forum, adds the `docs`, `dev` and `github` tags and starts syncing, with no config file or restart.

To make forums by hand instead:

1. Right-click the channel list → **Create Category** → `PROJECTS`. Optionally make it private for the team role.
2. For each project: right-click the category → **Create Channel** → **Forum**, and name it after the project. Then run `/project create` with `forum:` set to it, or list it in `projects.yaml`.
3. Either way, these forum settings are worth changing in ⚙️ **Edit Channel**:

| Setting | Value | Why |
|---|---|---|
| Tags | `docs`, `dev`, `github`, plus your own, e.g. `discussion`, `bug`, `idea` | The bot applies the first three automatically |
| Default Sort Order | Recently Active | Updated docs rise to the top |
| Default Layout | List view | Easier to scan file names |
| Post Guidelines | "Docs and dev files are synced by the bot. Start your own post for discussions." | Tells the team what goes where |
| Permissions | The bot role can View Channel, Create Posts, Send Messages in Posts and Manage Threads | Without these the bot can't post. Add it explicitly if the category is private |

## 5. Collect the IDs

Turn on **Settings → Advanced → Developer Mode**, then right-click → **Copy ID** on:

- the **server** icon → `guild_id`
- each **forum** → `forum_channel_id`
- optionally a **role** (Server Settings → Roles) → `team_role_id`

These go into `projects.yaml` (see [03 · Configuration](03-configuration.md)). You only need forum IDs for projects you list there rather than making with `/project create`.

## 6. Other servers

Invite the bot with the same link, turn on Community, then run `/project create` there. Each server's projects, `/docs` lists, `/sync` and docsync tokens are kept separate.

- **Who can use `/project`:** members with **Manage Channels**, by default. Change this per server in **Server Settings → Integrations → Project Hub**, where you can also limit the other commands to a role. (`team_role_id` only applies to the home server.)
- **Private repos:** only the home server (`guild_id`) can mirror private repos. Other servers can only mirror **public** ones. Otherwise anyone who could add the bot could read every private repo the bot's GitHub token can see.
- **Removing the bot** from a server makes it forget that server's projects. The forums and posts stay.
