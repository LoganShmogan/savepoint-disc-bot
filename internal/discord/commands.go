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

// Bot handles slash commands.
type Bot struct {
	S        *discordgo.Session
	Cfg      *config.Config
	Store    *store.Store
	Sync     *filesync.Syncer
	Trigger  func(project string) // nil when GitHub sync is disabled
	APIURL   string               // shown in /token instructions
	download *http.Client
}

func NewBot(s *discordgo.Session, cfg *config.Config, st *store.Store, sync *filesync.Syncer) *Bot {
	b := &Bot{S: s, Cfg: cfg, Store: st, Sync: sync, download: &http.Client{Timeout: time.Minute}}
	s.AddHandler(b.onInteraction)
	return b
}

func projectOpt(required bool) *discordgo.ApplicationCommandOption {
	return &discordgo.ApplicationCommandOption{
		Type: discordgo.ApplicationCommandOptionString, Name: "project",
		Description: "Project name", Required: required, Autocomplete: true,
	}
}

var commands = []*discordgo.ApplicationCommand{
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

// Register installs the commands on the guild (guild commands update instantly).
func (b *Bot) Register() error {
	_, err := b.S.ApplicationCommandBulkOverwrite(b.S.State.User.ID, b.Cfg.GuildID, commands)
	return err
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
		if b.Cfg.TeamRoleID != "" && !slices.Contains(i.Member.Roles, b.Cfg.TeamRoleID) {
			b.reply(i, "You need the team role to use this bot.")
			return
		}
		data := i.ApplicationCommandData()
		switch data.Name {
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

func options(data discordgo.ApplicationCommandInteractionData) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, o := range data.Options {
		m[o.Name] = o
	}
	return m
}

func (b *Bot) autocomplete(i *discordgo.InteractionCreate) {
	var typed string
	for _, o := range i.ApplicationCommandData().Options {
		if o.Focused {
			typed = strings.ToLower(o.StringValue())
		}
	}
	var choices []*discordgo.ApplicationCommandOptionChoice
	for _, p := range b.Cfg.Projects {
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
	p, ok := b.Cfg.Project(o.StringValue())
	if !ok {
		b.reply(i, fmt.Sprintf("No project called `%s`. Add it to projects.yaml first.", o.StringValue()))
	}
	return p, ok
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
	link := b.Sync.PostURL(res.ThreadID)
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
		line := fmt.Sprintf("[`%s`](%s) · v%d · %s · <t:%d:R>", f.Path, b.Sync.PostURL(f.ThreadID), f.Version, f.Source, f.UpdatedAt.Unix())
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
		b.Trigger("")
		b.reply(i, "🔁 Checking GitHub for all projects…")
		return
	}
	p, ok := b.project(i, opts)
	if !ok {
		return
	}
	if p.Repo == "" {
		b.reply(i, fmt.Sprintf("**%s** has no repo set in projects.yaml.", p.Name))
		return
	}
	b.Trigger(p.Name)
	b.reply(i, fmt.Sprintf("🔁 Checking `%s` now. Updates land in the forum within a minute.", p.Repo))
}

func (b *Bot) token(i *discordgo.InteractionCreate) {
	raw := make([]byte, 24)
	rand.Read(raw)
	tok := "phb_" + hex.EncodeToString(raw)
	if err := b.Store.PutToken(filesync.Hash([]byte(tok)), i.Member.User.ID, i.Member.DisplayName()); err != nil {
		b.reply(i, "❌ "+err.Error())
		return
	}
	b.reply(i, fmt.Sprintf("Your docsync token (only shown once; this replaces any older token):\n```\ndocsync login --server %s --token %s\n```\nYou need to be on the Tailscale network to reach the server.", b.APIURL, tok))
}
