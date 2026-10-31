package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

type nopPoster struct{ n int }

func (p *nopPoster) CreatePost(context.Context, string, string, string, string, filesync.Attachment) (string, string, error) {
	p.n++
	return "t1", "t1", nil
}
func (*nopPoster) ReplacePost(context.Context, string, string, string, filesync.Attachment) error {
	return nil
}
func (*nopPoster) Reply(context.Context, string, string) error { return nil }
func (*nopPoster) ArchivePost(context.Context, string) error   { return nil }

func TestCLIFlow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.PutProject(&config.Project{GuildID: "g", Name: "demo", ForumChannelID: "f", Docs: []string{"docs/**"}})
	sync := &filesync.Syncer{Store: st, Poster: &nopPoster{}, MaxBytes: 16}
	srv := httptest.NewServer((&Server{Store: st, Sync: sync}).Handler())
	defer srv.Close()
	st.PutToken(filesync.Hash([]byte("tok")), "u1", "logan", "g")
	st.PutToken(filesync.Hash([]byte("other")), "u2", "someone", "g2")

	do := func(method, path string, body []byte, auth string) *http.Response {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+auth)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	check := func(content string) filesync.Status {
		body, _ := json.Marshal([]CheckFile{{Path: `docs\a.md`, SHA256: filesync.Hash([]byte(content))}})
		var out []CheckResult
		json.NewDecoder(do("POST", "/api/projects/demo/check", body, "tok").Body).Decode(&out)
		if len(out) != 1 || out[0].Path != "docs/a.md" {
			t.Fatalf("check result: %+v", out)
		}
		return out[0].Status
	}

	if r := do("GET", "/api/projects", nil, "wrong"); r.StatusCode != 401 {
		t.Fatalf("bad token: %d", r.StatusCode)
	}
	if r := do("POST", "/api/projects/demo/check", []byte("[]"), "other"); r.StatusCode != 404 {
		t.Fatalf("another server's token reached demo: %d", r.StatusCode)
	}
	if s := check("hello"); s != filesync.Created {
		t.Fatalf("new file: %s", s)
	}
	var up UploadResult
	json.NewDecoder(do("PUT", "/api/projects/demo/files/docs/a.md", []byte("hello"), "tok").Body).Decode(&up)
	if up.Status != filesync.Created || up.URL != "https://discord.com/channels/g/t1" {
		t.Fatalf("upload: %+v", up)
	}
	if s := check("hello"); s != filesync.Unchanged {
		t.Fatalf("re-check: %s", s)
	}
	if s := check("hello v2"); s != filesync.Updated {
		t.Fatalf("changed: %s", s)
	}
	if r := do("PUT", "/api/projects/demo/files/docs/big.md", bytes.Repeat([]byte("x"), 17), "tok"); r.StatusCode != 413 {
		t.Fatalf("too large: %d", r.StatusCode)
	}
	rec, _ := st.GetFile("g/demo", "docs/a.md")
	if rec.UpdatedBy != "logan" || rec.Source != store.SourceCLI {
		t.Fatalf("attribution: %+v", rec)
	}
	rec.Source = store.SourceGitHub
	st.PutFile(rec)
	if s := check("mine"); s != filesync.Blocked {
		t.Fatalf("github-managed: %s", s)
	}
}
