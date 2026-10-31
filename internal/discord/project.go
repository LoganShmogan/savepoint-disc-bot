package discord

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/bwmarrin/discordgo"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
)

// forumTags are the tags the bot applies to posts (config kinds plus the
// activity feed).
var forumTags = []string{config.KindDocs, config.KindDev, "github"}

var defaultDocs = []string{"docs/**", "README.md"}

func (b *Bot) projectCmd(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	switch data.Options[0].Name {
	case "create":
		b.projectCreate(i, data)
	case "edit":
		b.projectEdit(i, data)
	case "remove":
		b.projectRemove(i, data)
	}
}

// globs splits a comma-separated option; "-" clears the list.
func globs(o *discordgo.ApplicationCommandInteractionDataOption) []string {
	var out []string
	for _, g := range strings.Split(o.StringValue(), ",") {
		if g = strings.TrimSpace(g); g != "" && g != "-" {
			out = append(out, g)
		}
	}
	return out
}

// applySettings copies the shared create/edit options onto p.
func applySettings(p *config.Project, opts map[string]*discordgo.ApplicationCommandInteractionDataOption) {
	if o := opts["repo"]; o != nil {
		p.Repo = strings.TrimPrefix(strings.TrimSpace(o.StringValue()), "https://github.com/")
		if p.Repo == "-" {
			p.Repo = ""
		}
	}
	if o := opts["branch"]; o != nil {
		p.Branch = o.StringValue()
	}
	if o := opts["docs"]; o != nil {
		p.Docs = globs(o)
	}
	if o := opts["dev_files"]; o != nil {
		p.DevFiles = globs(o)
	}
}

// validate normalises p and, when it has a repo, checks GitHub can be read
// and this server is allowed to mirror it.
func (b *Bot) validate(p *config.Project) error {
	if err := p.Normalize(); err != nil {
		return err
	}
	if p.Repo == "" || b.CheckRepo == nil {
		return nil
	}
	return b.CheckRepo(context.Background(), p)
}

// fromYAML reports whether p is defined in projects.yaml, which overwrites
// slash-command changes on every restart.
func (b *Bot) fromYAML(p *config.Project) bool {
	return p.GuildID == b.Cfg.GuildID && slices.ContainsFunc(b.Cfg.Projects, func(c config.Project) bool { return c.Name == p.Name })
}

func (b *Bot) projectCreate(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	opts := options(data)
	p := &config.Project{GuildID: i.GuildID, Name: opts["name"].StringValue(), ForumChannelID: "pending"}
	applySettings(p, opts)
	if p.Repo != "" && opts["docs"] == nil {
		p.Docs = defaultDocs
	}
	b.deferReply(i)

	existing, err := b.Store.GetProject(p.GuildID, p.Name)
	if err != nil {
		b.edit(i, "❌ "+err.Error())
		return
	}
	if existing != nil {
		b.edit(i, fmt.Sprintf("❌ **%s** already exists here (<#%s>). Use `/project edit` to change it.",
			existing.Name, existing.ForumChannelID))
		return
	}
	if err := b.validate(p); err != nil {
		b.edit(i, "❌ "+err.Error())
		return
	}

	created := false
	if o := opts["forum"]; o != nil {
		p.ForumChannelID = o.ChannelValue(nil).ID
	} else {
		data := discordgo.GuildChannelCreateData{Name: p.Name, Type: discordgo.ChannelTypeGuildForum, Topic: topic(p)}
		if o := opts["category"]; o != nil {
			data.ParentID = o.ChannelValue(nil).ID
		}
		ch, err := b.S.GuildChannelCreateComplex(p.GuildID, data)
		if err != nil {
			b.edit(i, "❌ Couldn't create the forum: "+permHint(err))
			return
		}
		p.ForumChannelID, created = ch.ID, true
	}
	if err := b.ensureTags(p.ForumChannelID); err != nil {
		slog.Warn("couldn't add forum tags", "forum", p.ForumChannelID, "err", err)
	}
	if err := b.Store.PutProject(p); err != nil {
		b.edit(i, "❌ "+err.Error())
		return
	}

	verb := "Using"
	if created {
		verb = "Made"
	}
	msg := fmt.Sprintf("✅ %s <#%s> for **%s**.", verb, p.ForumChannelID, p.Name)
	switch {
	case p.Repo != "" && b.Trigger != nil:
		b.Trigger(p.Key())
		msg += fmt.Sprintf(" Mirroring `%s` (%s) now; files land within a minute.", p.Repo, p.Branch)
	case p.Repo != "":
		msg += " GitHub sync is off on the bot's server, so only uploads will work for now."
	default:
		msg += " Add files with `/upload`, or link a repo with `/project edit`."
	}
	b.edit(i, msg)
}

func (b *Bot) projectEdit(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	opts := options(data)
	p, ok := b.project(i, opts)
	if !ok {
		return
	}
	if b.fromYAML(p) {
		b.reply(i, fmt.Sprintf("**%s** is set up in projects.yaml on the bot's server. Change it there.", p.Name))
		return
	}
	applySettings(p, opts)
	b.deferReply(i)
	if err := b.validate(p); err != nil {
		b.edit(i, "❌ "+err.Error())
		return
	}
	if err := b.Store.PutProject(p); err != nil {
		b.edit(i, "❌ "+err.Error())
		return
	}
	if _, err := b.S.ChannelEdit(p.ForumChannelID, &discordgo.ChannelEdit{Topic: topic(p)}); err != nil {
		slog.Warn("couldn't update forum topic", "forum", p.ForumChannelID, "err", err)
	}
	if p.Repo != "" && b.Trigger != nil {
		b.Trigger(p.Key())
	}
	b.edit(i, fmt.Sprintf("✅ Updated **%s**: %s", p.Name, summary(p)))
}

func (b *Bot) projectRemove(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	p, ok := b.project(i, options(data))
	if !ok {
		return
	}
	if b.fromYAML(p) {
		b.reply(i, fmt.Sprintf("**%s** is set up in projects.yaml on the bot's server. Remove it there.", p.Name))
		return
	}
	if err := b.Store.DeleteProject(p.GuildID, p.Name); err != nil {
		b.reply(i, "❌ "+err.Error())
		return
	}
	b.reply(i, fmt.Sprintf("🗑️ Stopped syncing **%s**. <#%s> and its posts are untouched; delete the forum yourself if you no longer need it.",
		p.Name, p.ForumChannelID))
}

// ensureTags adds any of the bot's tags the forum is missing.
func (b *Bot) ensureTags(forumID string) error {
	ch, err := b.S.Channel(forumID)
	if err != nil {
		return err
	}
	tags := ch.AvailableTags
	for _, name := range forumTags {
		if !slices.ContainsFunc(tags, func(t discordgo.ForumTag) bool { return strings.EqualFold(t.Name, name) }) {
			tags = append(tags, discordgo.ForumTag{Name: name})
		}
	}
	if len(tags) == len(ch.AvailableTags) {
		return nil
	}
	_, err = b.S.ChannelEdit(forumID, &discordgo.ChannelEdit{AvailableTags: &tags})
	return err
}

func topic(p *config.Project) string {
	if p.Repo == "" {
		return "Project files, synced by Project Hub. Add files with /upload."
	}
	return fmt.Sprintf("Docs and activity from https://github.com/%s, synced by Project Hub.", p.Repo)
}

func summary(p *config.Project) string {
	if p.Repo == "" {
		return "uploads only, no repo"
	}
	s := fmt.Sprintf("`%s` on `%s`", p.Repo, p.Branch)
	if len(p.Docs) > 0 {
		s += ", docs `" + strings.Join(p.Docs, "`, `") + "`"
	}
	if len(p.DevFiles) > 0 {
		s += ", dev files `" + strings.Join(p.DevFiles, "`, `") + "`"
	}
	return s
}

// permHint explains the usual cause of a failed channel create.
func permHint(err error) string {
	var re *discordgo.RESTError
	if errors.As(err, &re) && re.Message != nil && re.Message.Code == discordgo.ErrCodeMissingPermissions {
		return "I need the **Manage Channels** permission in this server (or category)."
	}
	return err.Error() + "\nForums only exist in Community servers (Server Settings → Enable Community)."
}
