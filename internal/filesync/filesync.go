// Package filesync is the single path every file source (GitHub mirror,
// /upload, docsync CLI) goes through: hash, dedupe, then create or replace
// the file's forum post.
package filesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

// ErrPostGone is returned by a Poster when the post was deleted in Discord;
// Upsert then creates a fresh one.
var ErrPostGone = errors.New("forum post no longer exists")

type Attachment struct {
	Name    string
	Content []byte
}

// Poster is the Discord side of a sync. internal/discord implements it.
type Poster interface {
	CreatePost(ctx context.Context, forumID, title, tag, header string, file Attachment) (threadID, msgID string, err error)
	ReplacePost(ctx context.Context, threadID, msgID, header string, file Attachment) error
	Reply(ctx context.Context, threadID, content string) error
	ArchivePost(ctx context.Context, threadID string) error
}

type Request struct {
	Project *config.Project
	Path    string
	Content []byte
	Source  store.Source
	User    string // display name credited for the change
	GitSHA  string // blob SHA, GitHub only
	Commit  string // commit SHA, GitHub only
	Force   bool   // overwrite a GitHub-managed file anyway
}

type Status string

const (
	Created   Status = "created"
	Updated   Status = "updated"
	Unchanged Status = "unchanged"
	Blocked   Status = "github-managed" // owned by the GitHub mirror and Force not set
	TooLarge  Status = "too-large"
)

type Result struct {
	Status   Status
	Version  int
	ThreadID string
}

type Syncer struct {
	Store    *store.Store
	Poster   Poster
	MaxBytes int
	GuildID  string

	mu sync.Mutex // serialises upserts so two sources can't race on one post
}

func Hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// CleanPath normalises a user-supplied path to a relative, slash-separated
// form and rejects anything escaping the project root.
func CleanPath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	p = strings.TrimLeft(path.Clean("/"+p), "/")
	if p == "" || p == "." {
		return "", errors.New("empty path")
	}
	return p, nil
}

// Check reports what Upsert would do with content at path, without posting.
func (s *Syncer) Check(project, p, sum string) (Status, error) {
	rec, err := s.Store.GetFile(project, p)
	switch {
	case err != nil:
		return "", err
	case rec == nil:
		return Created, nil
	case rec.SHA256 == sum && !rec.Removed:
		return Unchanged, nil
	case rec.Source == store.SourceGitHub && !rec.Removed:
		return Blocked, nil
	default:
		return Updated, nil
	}
}

func (s *Syncer) Upsert(ctx context.Context, req Request) (Result, error) {
	p, err := CleanPath(req.Path)
	if err != nil {
		return Result{}, err
	}
	if s.MaxBytes > 0 && len(req.Content) > s.MaxBytes {
		return Result{Status: TooLarge}, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	proj := req.Project.Name
	sum := Hash(req.Content)
	rec, err := s.Store.GetFile(proj, p)
	if err != nil {
		return Result{}, err
	}

	if rec != nil && !rec.Removed {
		if rec.SHA256 == sum {
			if rec.GitSHA != req.GitSHA && req.GitSHA != "" {
				rec.GitSHA = req.GitSHA // same bytes, e.g. a re-uploaded file later committed
				if err := s.Store.PutFile(rec); err != nil {
					return Result{}, err
				}
			}
			return Result{Status: Unchanged, Version: rec.Version, ThreadID: rec.ThreadID}, nil
		}
		if rec.Source == store.SourceGitHub && req.Source != store.SourceGitHub && !req.Force {
			return Result{Status: Blocked, Version: rec.Version, ThreadID: rec.ThreadID}, nil
		}
	}

	next := &store.File{
		Project: proj, Path: p, SHA256: sum, GitSHA: req.GitSHA, Version: 1,
		Source: req.Source, UpdatedBy: req.User, UpdatedAt: time.Now(),
	}
	file := Attachment{Name: path.Base(p), Content: req.Content}

	status := Created
	if rec != nil {
		next.Version = rec.Version + 1
		header := s.header(next, req.Commit)
		err := s.Poster.ReplacePost(ctx, rec.ThreadID, rec.MsgID, header, file)
		if err == nil {
			next.ThreadID, next.MsgID = rec.ThreadID, rec.MsgID
			status = Updated
			note := fmt.Sprintf("🔄 Updated to **v%d** by %s via %s", next.Version, req.User, sourceLabel(req.Source, req.Commit))
			if err := s.Poster.Reply(ctx, rec.ThreadID, note); err != nil {
				return Result{}, fmt.Errorf("post update note: %w", err)
			}
		} else if !errors.Is(err, ErrPostGone) {
			return Result{}, fmt.Errorf("replace post: %w", err)
		}
	}
	if next.ThreadID == "" {
		kind, _ := req.Project.Match(p)
		next.ThreadID, next.MsgID, err = s.Poster.CreatePost(ctx, req.Project.ForumChannelID,
			Title(p), kind, s.header(next, req.Commit), file)
		if err != nil {
			return Result{}, fmt.Errorf("create post: %w", err)
		}
	}
	if err := s.Store.PutFile(next); err != nil {
		return Result{}, err
	}
	return Result{Status: status, Version: next.Version, ThreadID: next.ThreadID}, nil
}

// MarkRemoved archives the post of a file deleted from the repo.
func (s *Syncer) MarkRemoved(ctx context.Context, rec *store.File, commit string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	msg := fmt.Sprintf("🗑️ Removed from the repo in `%s`. This post is archived; it comes back if the file returns.", short(commit))
	if err := s.Poster.Reply(ctx, rec.ThreadID, msg); err != nil && !errors.Is(err, ErrPostGone) {
		return err
	}
	if err := s.Poster.ArchivePost(ctx, rec.ThreadID); err != nil && !errors.Is(err, ErrPostGone) {
		return err
	}
	rec.Removed = true
	return s.Store.PutFile(rec)
}

func (s *Syncer) PostURL(threadID string) string {
	return fmt.Sprintf("https://discord.com/channels/%s/%s", s.GuildID, threadID)
}

func (s *Syncer) header(f *store.File, commit string) string {
	return fmt.Sprintf("📄 **%s** · v%d\nFrom %s · by %s · <t:%d:R>",
		f.Path, f.Version, sourceLabel(f.Source, commit), f.UpdatedBy, f.UpdatedAt.Unix())
}

// Title fits a path into Discord's 100-char post title, keeping the tail.
func Title(p string) string {
	r := []rune(p)
	if len(r) <= 100 {
		return p
	}
	return "…" + string(r[len(r)-99:])
}

func sourceLabel(src store.Source, commit string) string {
	switch src {
	case store.SourceGitHub:
		return "GitHub `" + short(commit) + "`"
	case store.SourceCLI:
		return "docsync CLI"
	default:
		return "Discord upload"
	}
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
