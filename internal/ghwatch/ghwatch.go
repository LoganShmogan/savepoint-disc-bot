// Package ghwatch polls each project's GitHub repo, mirrors matching docs and
// dev files into Discord, and builds the activity feed.
package ghwatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/go-github/v76/github"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

type Event struct {
	Title       string
	URL         string
	Description string
	Author      string
	Color       int
	Time        time.Time
}

const (
	colorCommit  = 0x5865F2
	colorOpened  = 0x57F287
	colorMerged  = 0x9B59B6
	colorClosed  = 0x99AAB5
	colorRelease = 0xFEE75C
)

// Notifier is the Discord side of the activity feed.
type Notifier interface {
	CreateActivityPost(ctx context.Context, forumID, repo string) (threadID string, err error)
	PostEvents(ctx context.Context, threadID string, events []Event) error
}

type Watcher struct {
	GH       *github.Client
	Cfg      *config.Config // GuildID is the only server allowed to mirror private repos
	Store    *store.Store
	Sync     *filesync.Syncer
	Notify   Notifier
	Interval time.Duration

	trigger chan string
}

func New(token string, cfg *config.Config, st *store.Store, sync *filesync.Syncer, n Notifier) *Watcher {
	return &Watcher{
		GH: github.NewClient(nil).WithAuthToken(token), Cfg: cfg, Store: st, Sync: sync, Notify: n,
		Interval: time.Duration(cfg.PollMins) * time.Minute,
		trigger:  make(chan string, 8),
	}
}

// Trigger queues an immediate poll. only is a project key (guild/name) for
// one project, a guild ID for all of a server's projects, or "" for all.
func (w *Watcher) Trigger(only string) {
	select {
	case w.trigger <- only:
	default: // a poll is already queued
	}
}

func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	w.pollAll(ctx, "")
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.pollAll(ctx, "")
		case only := <-w.trigger:
			w.pollAll(ctx, only)
		}
	}
}

func (w *Watcher) pollAll(ctx context.Context, only string) {
	projects, err := w.Store.ListProjects("")
	if err != nil {
		slog.Error("list projects", "err", err)
		return
	}
	for _, p := range projects {
		if p.Repo == "" || (only != "" && only != p.Key() && only != p.GuildID) {
			continue
		}
		if err := w.poll(ctx, p); err != nil {
			slog.Error("github poll failed", "guild", p.GuildID, "project", p.Name, "repo", p.Repo, "err", err)
		}
	}
}

// ErrPrivateRepo stops other servers from mirroring repos that only the
// bot's GitHub token can see.
var ErrPrivateRepo = errors.New("private repos can only be used in the home server; make the repo public or use a public one")

// CheckRepo confirms the repo and branch exist and that the project's server
// may mirror it.
func (w *Watcher) CheckRepo(ctx context.Context, p *config.Project) error {
	owner, repo := p.OwnerRepo()
	r, _, err := w.GH.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return fmt.Errorf("can't find %s on GitHub: %w", p.Repo, err)
	}
	if r.GetPrivate() && p.GuildID != w.Cfg.GuildID {
		return ErrPrivateRepo
	}
	if _, _, err := w.GH.Repositories.GetBranch(ctx, owner, repo, p.Branch, 1); err != nil {
		return fmt.Errorf("can't find branch %s in %s: %w", p.Branch, p.Repo, err)
	}
	return nil
}

func (w *Watcher) poll(ctx context.Context, p *config.Project) error {
	owner, repo := p.OwnerRepo()
	if p.GuildID != w.Cfg.GuildID {
		// Re-checked every poll in case a public repo was made private.
		r, _, err := w.GH.Repositories.Get(ctx, owner, repo)
		if err != nil {
			return fmt.Errorf("get repo: %w", err)
		}
		if r.GetPrivate() {
			return ErrPrivateRepo
		}
	}
	st, err := w.Store.GetGHState(p.Key())
	if err != nil {
		return err
	}
	started := time.Now()

	br, _, err := w.GH.Repositories.GetBranch(ctx, owner, repo, p.Branch, 1)
	if err != nil {
		return fmt.Errorf("get branch %s: %w", p.Branch, err)
	}
	head := br.GetCommit().GetSHA()

	if head != st.LastCommitSHA {
		if err := w.mirror(ctx, p, head); err != nil {
			return err
		}
	}

	if st.ActivityThreadID == "" {
		if st.ActivityThreadID, err = w.Notify.CreateActivityPost(ctx, p.ForumChannelID, p.Repo); err != nil {
			return fmt.Errorf("create activity post: %w", err)
		}
	}
	// First run: start the feed from now rather than replaying history.
	if !st.LastActivityAt.IsZero() {
		events, err := w.activity(ctx, p, st, head, started)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			err := w.Notify.PostEvents(ctx, st.ActivityThreadID, events)
			if errors.Is(err, filesync.ErrPostGone) {
				// Someone deleted the activity post; make a new one.
				if st.ActivityThreadID, err = w.Notify.CreateActivityPost(ctx, p.ForumChannelID, p.Repo); err == nil {
					err = w.Notify.PostEvents(ctx, st.ActivityThreadID, events)
				}
			}
			if err != nil {
				return fmt.Errorf("post activity: %w", err)
			}
		}
	}

	st.LastCommitSHA, st.LastActivityAt = head, started
	return w.Store.PutGHState(st)
}

// mirror syncs every file at head matching the project's globs, fetching only
// blobs whose git SHA changed, and archives posts for files that disappeared.
func (w *Watcher) mirror(ctx context.Context, p *config.Project, head string) error {
	owner, repo := p.OwnerRepo()
	tree, _, err := w.GH.Git.GetTree(ctx, owner, repo, head, true)
	if err != nil {
		return fmt.Errorf("get tree: %w", err)
	}
	if tree.GetTruncated() {
		slog.Warn("repo tree truncated by GitHub; some files may be skipped", "repo", p.Repo)
	}

	present := map[string]bool{}
	for _, e := range tree.Entries {
		path := e.GetPath()
		if e.GetType() != "blob" {
			continue
		}
		if _, ok := p.Match(path); !ok {
			continue
		}
		present[path] = true

		rec, err := w.Store.GetFile(p.Key(), path)
		if err != nil {
			return err
		}
		if rec != nil && !rec.Removed && rec.GitSHA == e.GetSHA() {
			continue
		}
		if w.Sync.MaxBytes > 0 && e.GetSize() > w.Sync.MaxBytes {
			slog.Warn("skipping file over Discord size limit", "project", p.Name, "path", path, "bytes", e.GetSize())
			continue
		}
		raw, _, err := w.GH.Git.GetBlobRaw(ctx, owner, repo, e.GetSHA())
		if err != nil {
			return fmt.Errorf("get blob %s: %w", path, err)
		}
		res, err := w.Sync.Upsert(ctx, filesync.Request{
			Project: p, Path: path, Content: raw, Source: store.SourceGitHub,
			User: "GitHub", GitSHA: e.GetSHA(), Commit: head,
		})
		if err != nil {
			return fmt.Errorf("sync %s: %w", path, err)
		}
		slog.Info("mirrored", "project", p.Name, "path", path, "status", res.Status, "version", res.Version)
	}

	files, err := w.Store.ListFiles(p.Key())
	if err != nil {
		return err
	}
	for _, rec := range files {
		if rec.Source == store.SourceGitHub && !rec.Removed && !present[rec.Path] {
			if err := w.Sync.MarkRemoved(ctx, rec, head); err != nil {
				return fmt.Errorf("mark %s removed: %w", rec.Path, err)
			}
			slog.Info("archived removed file", "project", p.Name, "path", rec.Path)
		}
	}
	return nil
}

// activity collects commits, PRs, issues and releases in (since, until].
func (w *Watcher) activity(ctx context.Context, p *config.Project, st *store.GHState, head string, until time.Time) ([]Event, error) {
	owner, repo := p.OwnerRepo()
	since := st.LastActivityAt
	in := func(t *github.Timestamp) bool {
		return t != nil && t.After(since) && !t.After(until)
	}
	var events []Event

	if st.LastCommitSHA != "" && st.LastCommitSHA != head {
		cmp, _, err := w.GH.Repositories.CompareCommits(ctx, owner, repo, st.LastCommitSHA, head, &github.ListOptions{PerPage: 100})
		if err != nil {
			// Usually a force-push removed the old commit; the mirror already caught up.
			slog.Warn("compare commits failed; skipping commit summary", "repo", p.Repo, "err", err)
		} else if ev, ok := commitEvent(p, cmp, until); ok {
			events = append(events, ev)
		}
	}

	issues, _, err := w.GH.Issues.ListByRepo(ctx, owner, repo, &github.IssueListByRepoOptions{
		State: "all", Sort: "updated", Direction: "desc", Since: since,
		ListOptions: github.ListOptions{PerPage: 50},
	})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	for _, is := range issues {
		ref := fmt.Sprintf("#%d %s", is.GetNumber(), is.GetTitle())
		base := Event{URL: is.GetHTMLURL(), Author: is.GetUser().GetLogin()}
		add := func(title string, color int, at *github.Timestamp) {
			e := base
			e.Title, e.Color, e.Time = truncate(title, 256), color, at.Time
			events = append(events, e)
		}
		if is.IsPullRequest() {
			if in(is.CreatedAt) {
				add("🔀 PR opened: "+ref, colorOpened, is.CreatedAt)
			}
			if merged := is.GetPullRequestLinks().MergedAt; in(merged) {
				add("✅ PR merged: "+ref, colorMerged, merged)
			} else if merged == nil && in(is.ClosedAt) {
				add("❌ PR closed: "+ref, colorClosed, is.ClosedAt)
			}
		} else {
			if in(is.CreatedAt) {
				add("🐛 Issue opened: "+ref, colorOpened, is.CreatedAt)
			}
			if in(is.ClosedAt) {
				add("✔️ Issue closed: "+ref, colorClosed, is.ClosedAt)
			}
		}
	}

	rels, _, err := w.GH.Repositories.ListReleases(ctx, owner, repo, &github.ListOptions{PerPage: 10})
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}
	for _, r := range rels {
		if !r.GetDraft() && in(r.PublishedAt) {
			name := r.GetName()
			if name == "" {
				name = r.GetTagName()
			}
			events = append(events, Event{
				Title: truncate("🚀 Release: "+name, 256), URL: r.GetHTMLURL(), Author: r.GetAuthor().GetLogin(),
				Description: truncate(r.GetBody(), 1000), Color: colorRelease, Time: r.PublishedAt.Time,
			})
		}
	}

	sort.SliceStable(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	return events, nil
}

func commitEvent(p *config.Project, cmp *github.CommitsComparison, at time.Time) (Event, bool) {
	commits := cmp.Commits
	if len(commits) == 0 {
		return Event{}, false
	}
	const show = 10
	var b strings.Builder
	start := max(0, len(commits)-show)
	for _, c := range commits[start:] {
		msg, _, _ := strings.Cut(c.GetCommit().GetMessage(), "\n")
		author := c.GetAuthor().GetLogin()
		if author == "" {
			author = c.GetCommit().GetAuthor().GetName()
		}
		fmt.Fprintf(&b, "[`%s`](%s) %s — %s\n", c.GetSHA()[:7], c.GetHTMLURL(), truncate(msg, 80), author)
	}
	if start > 0 {
		fmt.Fprintf(&b, "…and %d earlier", start)
	}
	noun := "commits"
	if len(commits) == 1 {
		noun = "commit"
	}
	return Event{
		Title: fmt.Sprintf("🔨 %d new %s to %s", len(commits), noun, p.Branch),
		URL:   cmp.GetHTMLURL(), Description: b.String(), Color: colorCommit, Time: at,
	}, true
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
