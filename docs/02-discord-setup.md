# 02 · Discord setup

This is a one-time setup for the Discord application and the project forums.

## 1. Create the application

1. Go to <https://discord.com/developers/applications> → **New Application**. Name it after Save Point (ours is "SavePoint") and upload the Save Point logo as its icon.
2. Under **General Information**, set:
   - **Terms of Service URL:** `https://savepoint.co.nz/legal/discord-bot#terms`
   - **Privacy Policy URL:** `https://savepoint.co.nz/legal/discord-bot#privacy`
3. Under **Bot**:
   - **Reset Token** and put the token in `.env` as `DISCORD_TOKEN`. Never paste it in chat or commit it.
   - Turn **Public Bot** off, so only Save Point can invite it.
   - Leave all **Privileged Gateway Intents** off. The bot doesn't need them.

## 2. Invite it

Replace `APPLICATION_ID` with the ID from **General Information**, then open the link and pick the Save Point server:

```
https://discord.com/oauth2/authorize?client_id=APPLICATION_ID&scope=bot+applications.commands&permissions=326417632256
```

`326417632256` grants exactly these permissions: View Channels, Send Messages, Send Messages in Threads, Create Public Threads, Manage Threads, Embed Links, Attach Files and Read Message History.

## 3. Turn on Community

Discord only offers forum channels in **Community** servers:
**Server Settings → Enable Community → Get Started**. It asks for a rules channel and an updates channel. Community doesn't make the server public, so leave **Server Discovery** off.

## 4. Create the project forums

1. Right-click the channel list → **Create Category** → `PROJECTS`. Optionally make it private for the team role.
2. For each project: right-click the category → **Create Channel** → **Forum**, and name it after the project.
3. In each forum, open ⚙️ **Edit Channel**:

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

These go into `projects.yaml` (see [03 · Configuration](03-configuration.md)).
