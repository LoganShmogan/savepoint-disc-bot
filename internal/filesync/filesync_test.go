package filesync

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

type fakePoster struct {
	created, replaced, replies, archived int
	gone                                 bool // ReplacePost reports the post was deleted
	lastTag                              string
}

func (f *fakePoster) CreatePost(_ context.Context, _, _, tag, _ string, _ Attachment) (string, string, error) {
	f.created++
	f.lastTag = tag
	id := fmt.Sprintf("t%d", f.created)
	return id, id, nil
}

func (f *fakePoster) ReplacePost(context.Context, string, string, string, Attachment) error {
	if f.gone {
		return ErrPostGone
	}
	f.replaced++
	return nil
}

func (f *fakePoster) Reply(context.Context, string, string) error { f.replies++; return nil }
func (f *fakePoster) ArchivePost(context.Context, string) error   { f.archived++; return nil }

func setup(t *testing.T) (*Syncer, *fakePoster, *config.Project) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	fp := &fakePoster{}
	proj := &config.Project{GuildID: "g", Name: "demo", ForumChannelID: "f1", Docs: []string{"docs/**"}, DevFiles: []string{".env.example"}}
	return &Syncer{Store: st, Poster: fp, MaxBytes: 100}, fp, proj
}

func upsert(t *testing.T, s *Syncer, req Request) Result {
	t.Helper()
	res, err := s.Upsert(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestUpsertLifecycle(t *testing.T) {
	s, fp, proj := setup(t)
	req := Request{Project: proj, Path: "docs/a.md", Content: []byte("one"), Source: store.SourceUpload, User: "logan"}

	if r := upsert(t, s, req); r.Status != Created || r.Version != 1 || fp.created != 1 {
		t.Fatalf("first upload: %+v, created=%d", r, fp.created)
	}
	if r := upsert(t, s, req); r.Status != Unchanged || fp.replaced != 0 {
		t.Fatalf("same content: %+v, replaced=%d", r, fp.replaced)
	}
	req.Content = []byte("two")
	if r := upsert(t, s, req); r.Status != Updated || r.Version != 2 || fp.replaced != 1 || fp.replies != 1 {
		t.Fatalf("changed content: %+v, %+v", r, fp)
	}
	if fp.created != 1 {
		t.Fatalf("update must edit the existing post, created=%d", fp.created)
	}
}

func TestGitHubOwnership(t *testing.T) {
	s, _, proj := setup(t)
	upsert(t, s, Request{Project: proj, Path: "docs/a.md", Content: []byte("repo"), Source: store.SourceGitHub, User: "GitHub", GitSHA: "g1"})

	manual := Request{Project: proj, Path: "docs/a.md", Content: []byte("mine"), Source: store.SourceCLI, User: "sam"}
	if r := upsert(t, s, manual); r.Status != Blocked {
		t.Fatalf("CLI over a GitHub file should be blocked, got %s", r.Status)
	}
	manual.Force = true
	if r := upsert(t, s, manual); r.Status != Updated {
		t.Fatalf("forced CLI upload should update, got %s", r.Status)
	}
	// GitHub always wins back ownership.
	if r := upsert(t, s, Request{Project: proj, Path: "docs/a.md", Content: []byte("repo2"), Source: store.SourceGitHub, User: "GitHub", GitSHA: "g2"}); r.Status != Updated {
		t.Fatalf("GitHub update: %s", r.Status)
	}
}

func TestDeletedPostIsRecreated(t *testing.T) {
	s, fp, proj := setup(t)
	req := Request{Project: proj, Path: ".env.example", Content: []byte("A=1"), Source: store.SourceUpload, User: "logan"}
	upsert(t, s, req)
	if fp.lastTag != config.KindDev {
		t.Fatalf("tag = %q, want dev", fp.lastTag)
	}
	fp.gone = true
	req.Content = []byte("A=2")
	r := upsert(t, s, req)
	if fp.created != 2 || r.ThreadID != "t2" || r.Version != 2 {
		t.Fatalf("expected a new post at v2: %+v created=%d", r, fp.created)
	}
}

func TestRemovedThenRestored(t *testing.T) {
	s, fp, proj := setup(t)
	gh := Request{Project: proj, Path: "docs/a.md", Content: []byte("x"), Source: store.SourceGitHub, User: "GitHub"}
	upsert(t, s, gh)
	rec, _ := s.Store.GetFile(proj.Key(), "docs/a.md")
	if err := s.MarkRemoved(context.Background(), rec, "abc"); err != nil {
		t.Fatal(err)
	}
	if fp.archived != 1 {
		t.Fatal("post not archived")
	}
	if st, _ := s.Check(proj, "docs/a.md", Hash([]byte("y"))); st != Updated {
		t.Fatalf("removed GitHub file should accept uploads, got %s", st)
	}
	if r := upsert(t, s, gh); r.Status != Updated || fp.replaced != 1 {
		t.Fatalf("restoring identical content must revive the post: %+v", r)
	}
}

func TestTooLargeAndPaths(t *testing.T) {
	s, _, proj := setup(t)
	if r := upsert(t, s, Request{Project: proj, Path: "big.bin", Content: make([]byte, 101)}); r.Status != TooLarge {
		t.Fatalf("got %s", r.Status)
	}
	for in, want := range map[string]string{`docs\a.md`: "docs/a.md", "/x/../../y.md": "y.md", " ./a//b.md ": "a/b.md"} {
		if got, err := CleanPath(in); err != nil || got != want {
			t.Errorf("CleanPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := CleanPath(".."); err == nil {
		t.Error("CleanPath(..) should fail")
	}
}
