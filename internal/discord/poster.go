// Package discord implements forum posting (filesync.Poster, ghwatch.Notifier)
// and the bot's slash commands.
package discord

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/bwmarrin/discordgo"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/ghwatch"
)

// Poster talks to Discord's REST API on behalf of the sync code.
type Poster struct {
	S *discordgo.Session

	mu   sync.Mutex
	tags map[string]map[string]string // forum ID → lowercased tag name → tag ID
}

func NewPoster(s *discordgo.Session) *Poster {
	return &Poster{S: s, tags: map[string]map[string]string{}}
}

var noMentions = &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}

func ptr[T any](v T) *T { return &v }

// gone maps "unknown channel/message" errors to filesync.ErrPostGone.
func gone(err error) error {
	var re *discordgo.RESTError
	if errors.As(err, &re) && re.Message != nil &&
		(re.Message.Code == discordgo.ErrCodeUnknownChannel || re.Message.Code == discordgo.ErrCodeUnknownMessage) {
		return fmt.Errorf("%w: %v", filesync.ErrPostGone, err)
	}
	return err
}

// tagID looks up a forum tag by name, refetching the forum when it isn't
// cached so tags created after startup are picked up.
func (p *Poster) tagID(ctx context.Context, forumID, name string) string {
	p.mu.Lock()
	id, ok := p.tags[forumID][name]
	p.mu.Unlock()
	if ok {
		return id
	}
	ch, err := p.S.Channel(forumID, discordgo.WithContext(ctx))
	if err != nil {
		return ""
	}
	m := map[string]string{}
	for _, t := range ch.AvailableTags {
		m[strings.ToLower(t.Name)] = t.ID
	}
	p.mu.Lock()
	p.tags[forumID] = m
	p.mu.Unlock()
	return m[name]
}

func (p *Poster) startPost(ctx context.Context, forumID, title, tag string, msg *discordgo.MessageSend) (string, error) {
	start := &discordgo.ThreadStart{Name: title}
	if id := p.tagID(ctx, forumID, tag); id != "" {
		start.AppliedTags = []string{id}
	}
	msg.AllowedMentions = noMentions
	th, err := p.S.ForumThreadStartComplex(forumID, start, msg, discordgo.WithContext(ctx))
	if err != nil {
		return "", err
	}
	return th.ID, nil
}

func (p *Poster) CreatePost(ctx context.Context, forumID, title, tag, header string, f filesync.Attachment) (string, string, error) {
	id, err := p.startPost(ctx, forumID, title, tag, &discordgo.MessageSend{
		Content: header,
		Files:   []*discordgo.File{{Name: f.Name, Reader: bytes.NewReader(f.Content)}},
	})
	// A forum post's starter message shares the thread's ID.
	return id, id, err
}

func (p *Poster) ReplacePost(ctx context.Context, threadID, msgID, header string, f filesync.Attachment) error {
	// Edits fail on archived threads, and forum posts auto-archive when idle.
	if _, err := p.S.ChannelEdit(threadID, &discordgo.ChannelEdit{Archived: ptr(false)}, discordgo.WithContext(ctx)); err != nil {
		if err = gone(err); errors.Is(err, filesync.ErrPostGone) {
			return err
		}
	}
	_, err := p.S.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID: msgID, Channel: threadID, Content: &header, AllowedMentions: noMentions,
		Attachments: &[]*discordgo.MessageAttachment{}, // drop the old version
		Files:       []*discordgo.File{{Name: f.Name, Reader: bytes.NewReader(f.Content)}},
	}, discordgo.WithContext(ctx))
	return gone(err)
}

func (p *Poster) Reply(ctx context.Context, threadID, content string) error {
	_, err := p.S.ChannelMessageSendComplex(threadID, &discordgo.MessageSend{
		Content: content, AllowedMentions: noMentions,
	}, discordgo.WithContext(ctx))
	return gone(err)
}

func (p *Poster) ArchivePost(ctx context.Context, threadID string) error {
	_, err := p.S.ChannelEdit(threadID, &discordgo.ChannelEdit{Archived: ptr(true)}, discordgo.WithContext(ctx))
	return gone(err)
}

func (p *Poster) CreateActivityPost(ctx context.Context, forumID, repo string) (string, error) {
	id, err := p.startPost(ctx, forumID, "📡 GitHub Activity", "github", &discordgo.MessageSend{
		Content: fmt.Sprintf("Commits, pull requests, issues and releases for [`%s`](https://github.com/%s) show up here.", repo, repo),
	})
	if err != nil {
		return "", err
	}
	// Pinning needs Manage Threads; the feed still works without it.
	p.S.ChannelEdit(id, &discordgo.ChannelEdit{Flags: ptr(discordgo.ChannelFlagPinned)}, discordgo.WithContext(ctx))
	return id, nil
}

func (p *Poster) PostEvents(ctx context.Context, threadID string, events []ghwatch.Event) error {
	for len(events) > 0 {
		n := min(len(events), 10) // Discord's per-message embed limit
		embeds := make([]*discordgo.MessageEmbed, n)
		for i, e := range events[:n] {
			embeds[i] = &discordgo.MessageEmbed{
				Title: e.Title, URL: e.URL, Description: e.Description, Color: e.Color,
				Timestamp: e.Time.Format("2006-01-02T15:04:05Z07:00"),
			}
			if e.Author != "" {
				embeds[i].Author = &discordgo.MessageEmbedAuthor{Name: e.Author, URL: "https://github.com/" + e.Author}
			}
		}
		_, err := p.S.ChannelMessageSendComplex(threadID, &discordgo.MessageSend{Embeds: embeds}, discordgo.WithContext(ctx))
		if err != nil {
			return gone(err)
		}
		events = events[n:]
	}
	return nil
}
