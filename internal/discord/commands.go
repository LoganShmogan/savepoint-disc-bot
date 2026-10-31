package discord

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

// Bot handles slash commands in every server it has been added to.
type Bot struct {
	S         *discordgo.Session
	Cfg       *config.Config
	Store     *store.Store
	Sync      *filesync.Syncer
	Trigger   func(only string)                                  // nil when GitHub sync is disabled; see ghwatch.Trigger
	CheckRepo func(ctx context.Context, p *config.Project) error // nil when GitHub sync is disabled
	APIURL    string                                             // shown in /token instructions
	download  *http.Client
}

func NewBot(s *discordgo.Session, cfg *config.Config, st *store.Store, sync *filesync.Syncer) *Bot {
	b := &Bot{S: s, Cfg: cfg, Store: st, Sync: sync, download: &http.Client{Timeout: time.Minute}}
	s.AddHandler(b.onInteraction)
	s.AddHandler(b.onGuildDelete)
	return b
}

// onGuildDelete forgets a server's projects when the bot is removed from it.
// Unavailable means a Discord outage, not a removal.
func (b *Bot) onGuildDelete(_ *discordgo.Session, g *discordgo.GuildDelete) {
	if g.Unavailable {
		return
	}
	if err := b.Store.DeleteGuild(g.ID); err != nil {
		slog.Error("forget guild", "guild", g.ID, "err", err)
		return
	}
	slog.Info("removed from server; forgot its projects", "guild", g.ID)
}

func projectOpt(required bool) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionString, Name: "project",
		Description: "Project name", Required: required, Autocomplete: true,
	}
}

var (
	guildOnly    = &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild}
	manageForums = ptr(int64(discordgo.PermissionManageChannels))
)

func stringOpt(name, desc string) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{Type: discordgo.ApplicationCommandOptionString, Name: name, Description: desc}
}

// projectSettings are the options /project create and edit share.
func projectSettings() []*discordgo.ApplicationCommandOption {
	return []*discordgo.ApplicationCommandOption{
		stringOpt("repo", "GitHub repo to mirror, owner/name (\"-\" to clear)"),
		stringOpt("branch", "Branch to mirror (default: main)"),
		stringOpt("docs", "Comma-separated globs for docs (default: docs/**, README.md)"),
		stringOpt("dev_files", "Comma-separated globs for dev files, e.g. .env.example (\"-\" to clear)"),
	}
}

var commands = []*discordgo.ApplicationCommand{
	{
		Name: "project", Description: "Set up project forums in this server",
		DefaultMemberPermissions: manageForums,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type: discordgo.ApplicationCommandOptionSubCommand, Name: "create",
				Description: "Make a forum for a project and start syncing it",
				Options: append([]*discordgo.ApplicationCommandOption{
					{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Project name, e.g. memeline", Required: true, MaxLength: 50},
				}, append(projectSettings(),
					&discordgo.ApplicationCommandOption{
						Type: discordgo.ApplicationCommandOptionChannel, Name: "category", Description: "Category to put the new forum in",
						ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildCategory},
					},
					&discordgo.ApplicationCommandOption{
						Type: discordgo.ApplicationCommandOptionChannel, Name: "forum", Description: "Use this existing forum instead of making a new one",
						ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildForum},
					},
				)...),
			},
			{
				Type: discordgo.ApplicationCommandOptionSubCommand, Name: "edit",
				Description: "Change a project's repo, branch or file patterns",
				Options:     append([]*discordgo.ApplicationCommandOption{projectOpt(true)}, projectSettings()...),
			},
			{
				Type: discordgo.ApplicationCommandOptionSubCommand, Name: "remove",
				Description: "Stop syncing a project (its forum and posts are kept)",
				Options:     []*discordgo.ApplicationCommandOption{projectOpt(true)},
			},
		},
	},
	{
		Name: "upload", Description: "Upload a file to a project; replaces it if a newer version",
		Options: []*discordgo.ApplicationCommandOption{
			projectOpt(true),
			{Type: discordgo.ApplicationCommandOptionAttachment, Name: "file", Description: "The file", Required: true},
			{Type: discordgo.ApplicationCommandOptionString, Name: "path", Description: "Path to store it under, e.g. docs/setup.md (default: file name)"},
			{Type: discordgo.ApplicationCommandOptionBoolean, Name: "force", Description: "Overwrite even if the file is managed by GitHub"},
		},
	},
	{Name: "docs", Description: "List a project's files", Options: []*discordgo.ApplicationCommandOption{projectOpt(true)}},
	{Name: "sync", Description: "Check GitHub for changes now", Options: []*discordgo.ApplicationCommandOption{projectOpt(false)}},
	{Name: "token", Description: "Get a personal token for the docsync CLI (replaces your old one)"},
}

// Register installs the commands globally, so they work in every server the
// bot is added to.
func (b *Bot) Register() error {
	for _, c := range commands {
		c.Contexts = guildOnly
	}
	appID := b.S.State.User.ID
	if _, err := b.S.ApplicationCommandBulkOverwrite(appID, "", commands); err != nil {
		return err
	}
	// Older versions registered per-guild copies, which would now show twice.
	if b.Cfg.GuildID != "" {
		if _, err := b.S.ApplicationCommandBulkOverwrite(appID, b.Cfg.GuildID, nil); err != nil {
			slog.Warn("couldn't clear old guild commands", "err", err)
		}
	}
	return nil
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommandAutocomplete:
		b.autocomplete(i)
	case discordgo.InteractionApplicationCommand:
		if i.Member == nil {
			b.reply(i, "Use me inside the server, not in DMs.")
			return
		}
		// Other servers control access through Server Settings → Integrations.
		if i.GuildID == b.Cfg.GuildID && b.Cfg.TeamRoleID != "" && !slices.Contains(i.Member.Roles, b.Cfg.TeamRoleID) {
			b.reply(i, "You need the team role to use this bot.")
			return
		}
		data := i.ApplicationCommandData()
		switch data.Name {
		case "project":
			b.projectCmd(i, data)
		case "upload":
			b.upload(i, data)
		case "docs":
			b.docs(i, data)
		case "sync":
			b.sync(i, data)
		case "token":
			b.token(i)
		}
	}
}

// leafOptions returns a command's options, looking inside a subcommand if
// there is one.
func leafOptions(data discordgo.ApplicationCommandInteractionData) []*discordgo.ApplicationCommandInteractionDataOption {
	if len(data.Options) == 1 && data.Options[0].Type == discordgo.ApplicationCommandOptionSubCommand {
		return data.Options[0].Options
	}
	return data.Options
}

func options(data discordgo.ApplicationCommandInteractionData) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range leafOptions(data) {
		m[o.Name] = o
	}
	return m
}

func (b *Bot) autocomplete(i *discordgo.InteractionCreate) {
	var typed string
	for _, o := range leafOptions(i.ApplicationCommandData()) {
		if o.Focused {
			typed = strings.ToLower(o.StringValue())
		}
	}
	projects, err := b.Store.ListProjects(i.GuildID)
	if err != nil {
		slog.Error("autocomplete", "err", err)
	}
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, p := range projects {
		if strings.Contains(p.Name, typed) && len(choices) < 25 {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: p.Name, Value: p.Name})
		}
	}
	b.S.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{Choices: choices},
	})
}

// reply sends an immediate private response.
func (b *Bot) reply(i *discordgo.InteractionCreate, msg string) {
	err := b.S.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		slog.Error("interaction reply failed", "err", err)
	}
}

// deferReply acknowledges a slow command; finish it with b.edit.
func (b *Bot) deferReply(i *discordgo.InteractionCreate) {
	b.S.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
}

func (b *Bot) edit(i *discordgo.InteractionCreate, msg string, embeds ...*discordgo.MessageEmbed) {
	if _, err := b.S.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &msg, Embeds: &embeds}); err != nil {
		slog.Error("interaction edit failed", "err", err)
	}
}

func (b *Bot) project(i *discordgo.InteractionCreate, opts map[string]*discordgo.ApplicationCommandInteractionDataOption) (*config.Project, bool) {
	o := opts["project"]
	if o == nil {
		return nil, false
	}
	p, err := b.Store.GetProject(i.GuildID, o.StringValue())
	if err != nil {
		b.reply(i, "❌ "+err.Error())
		return nil, false
	}
	if p == nil {
		b.reply(i, fmt.Sprintf("No project called `%s` in this server. Make one with `/project create`.", o.StringValue()))
	}
	return p, p != nil
}

func (b *Bot) upload(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	opts := options(data)
	p, ok := b.project(i, opts)
	if !ok {
		return
	}
	att := data.Resolved.Attachments[opts["file"].Value.(string)]
	if att.Size > b.Sync.MaxBytes {
		b.reply(i, fmt.Sprintf("`%s` is %.1f MB, over the %d MB Discord limit. Commit it to GitHub and link it instead.",
			att.Filename, float64(att.Size)/(1<<20), b.Cfg.MaxFileMB))
		return
	}
	b.deferReply(i)

	path := att.Filename
	if o := opts["path"]; o != nil {
		path = o.StringValue()
		if strings.HasSuffix(path, "/") {
			path += att.Filename
		}
	}
	content, err := b.fetch(att.URL)
	if err != nil {
		b.edit(i, "❌ Couldn't download the attachment: "+err.Error())
		return
	}
	force := opts["force"] != nil && opts["force"].BoolValue()

	res, err := b.Sync.Upsert(context.Background(), filesync.Request{
		Project: p, Path: path, Content: content, Source: store.SourceUpload,
		User: i.Member.DisplayName(), Force: force,
	})
	if err != nil {
		slog.Error("upload failed", "project", p.Name, "path", path, "err", err)
		b.edit(i, "❌ Upload failed: "+err.Error())
		return
	}
	link := filesync.PostURL(p.GuildID, res.ThreadID)
	switch res.Status {
	case filesync.Created:
		b.edit(i, fmt.Sprintf("✅ Added `%s` to **%s**: %s", path, p.Name, link))
	case filesync.Updated:
		b.edit(i, fmt.Sprintf("🔄 Replaced `%s` with **v%d**: %s", path, res.Version, link))
	case filesync.Unchanged:
		b.edit(i, fmt.Sprintf("👍 `%s` is already up to date (v%d), so nothing changed: %s", path, res.Version, link))
	case filesync.Blocked:
		b.edit(i, fmt.Sprintf("🔒 `%s` is managed by GitHub. Push your change to `%s` instead, or re-run with `force: True`.", path, p.Repo))
	case filesync.TooLarge:
		b.edit(i, fmt.Sprintf("`%s` is over the %d MB limit.", path, b.Cfg.MaxFileMB))
	}
}

func (b *Bot) fetch(url string) ([]byte, error) {
	resp, err := b.download.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, int64(b.Sync.MaxBytes)+1))
}

func (b *Bot) docs(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	p, ok := b.project(i, options(data))
	if !ok {
		return
	}
	files, err := b.Store.ListFiles(p.Name)
	if err != nil {
		b.reply(i, "❌ "+err.Error())
		return
	}
	if len(files) == 0 {
		b.reply(i, fmt.Sprintf("**%s** has no files yet. Use `/upload` or push to `%s`.", p.Name, p.Repo))
		return
	}
	var lines []string
	for _, f := range files {
		line := fmt.Sprintf("[`%s`](%s) · v%d · %s · <t:%d:R>", f.Path, filesync.PostURL(p.GuildID, f.ThreadID), f.Version, f.Source, f.UpdatedAt.Unix())
		if f.Removed {
			line = "~~" + line + "~~ (removed)"
		}
		lines = append(lines, line)
	}
	// Embed descriptions max out at 4096 chars.
	var desc strings.Builder
	for n, l := range lines {
		if desc.Len()+len(l) > 3900 {
			fmt.Fprintf(&desc, "…and %d more", len(lines)-n)
			break
		}
		desc.WriteString(l + "\n")
	}
	b.S.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags:  discordgo.MessageFlagsEphemeral,
			Embeds: []*discordgo.MessageEmbed{{Title: fmt.Sprintf("%s · %d files", p.Name, len(files)), Description: desc.String()}},
		},
	})
}

func (b *Bot) sync(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	if b.Trigger == nil {
		b.reply(i, "GitHub sync is off. Set `GITHUB_TOKEN` on the server to enable it.")
		return
	}
	opts := options(data)
	if opts["project"] == nil {
		b.Trigger(i.GuildID)
		b.reply(i, "🔁 Checking GitHub for all of this server's projects…")
		return
	}
	p, ok := b.project(i, opts)
	if !ok {
		return
	}
	if p.Repo == "" {
		b.reply(i, fmt.Sprintf("**%s** has no repo. Add one with `/project edit`.", p.Name))
		return
	}
	b.Trigger(p.Key())
	b.reply(i, fmt.Sprintf("🔁 Checking `%s` now. Updates land in the forum within a minute.", p.Repo))
}

func (b *Bot) token(i *discordgo.InteractionCreate) {
	raw := make([]byte, 24)
	rand.Read(raw)
	tok := "phb_" + hex.EncodeToString(raw)
	if err := b.Store.PutToken(filesync.Hash([]byte(tok)), i.Member.User.ID, i.Member.DisplayName(), i.GuildID); err != nil {
		b.reply(i, "❌ "+err.Error())
		return
	}
	b.reply(i, fmt.Sprintf("Your docsync token for **this server's** projects (only shown once; this replaces any older token, from any server):\n```\ndocsync login --server %s --token %s\n```\nYou need to be on the Tailscale network to reach the server.", b.APIURL, tok))
}
