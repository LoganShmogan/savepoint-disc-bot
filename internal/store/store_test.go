package store

import (
	"path/filepath"
	"testing"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
)

func TestProjectsAndLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// A row from before projects were per-server.
	st.PutFile(&File{Project: "demo", Path: "a.md", Version: 1, Source: SourceGitHub})
	st.Close()

	if st, err = Open(path); err != nil { // reopening must not fail on the migration
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AdoptLegacy("home"); err != nil {
		t.Fatal(err)
	}
	if f, _ := st.GetFile("home/demo", "a.md"); f == nil {
		t.Fatal("legacy file not moved to the home server")
	}

	st.PutProject(&config.Project{GuildID: "g1", Name: "demo", ForumChannelID: "f1", Docs: []string{"docs/**"}})
	st.PutProject(&config.Project{GuildID: "g2", Name: "demo", ForumChannelID: "f2"})
	p, err := st.GetProject("g1", " Demo ")
	if err != nil || p == nil || p.ForumChannelID != "f1" || len(p.Docs) != 1 || len(p.DevFiles) != 0 {
		t.Fatalf("GetProject: %+v, %v", p, err)
	}
	if all, _ := st.ListProjects(""); len(all) != 2 {
		t.Fatalf("ListProjects(all) = %d", len(all))
	}
	st.DeleteGuild("g2")
	if mine, _ := st.ListProjects("g2"); len(mine) != 0 {
		t.Fatal("DeleteGuild left projects behind")
	}
}
