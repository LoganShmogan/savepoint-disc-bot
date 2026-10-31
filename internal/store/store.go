// Package store persists what the bot has posted, so restarts never repost
// files and changed files can be found by hash.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
)

const schema = `
CREATE TABLE IF NOT EXISTS files (
	project    TEXT NOT NULL,
	path       TEXT NOT NULL,
	sha256     TEXT NOT NULL,
	git_sha    TEXT NOT NULL DEFAULT '',
	version    INTEGER NOT NULL,
	source     TEXT NOT NULL,
	thread_id  TEXT NOT NULL,
	message_id TEXT NOT NULL,
	updated_by TEXT NOT NULL,
	updated_at INTEGER NOT NULL,
	removed    INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (project, path)
);
CREATE TABLE IF NOT EXISTS gh_state (
	project            TEXT PRIMARY KEY,
	last_commit_sha    TEXT NOT NULL DEFAULT '',
	activity_thread_id TEXT NOT NULL DEFAULT '',
	last_activity_at   INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS cli_tokens (
	token_hash TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL UNIQUE,
	user_name  TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS projects (
	guild_id         TEXT NOT NULL,
	name             TEXT NOT NULL,
	forum_channel_id TEXT NOT NULL,
	repo             TEXT NOT NULL DEFAULT '',
	branch           TEXT NOT NULL DEFAULT 'main',
	docs             TEXT NOT NULL DEFAULT '[]', -- JSON array of globs
	dev_files        TEXT NOT NULL DEFAULT '[]',
	PRIMARY KEY (guild_id, name)
);`

type Source string

const (
	SourceGitHub Source = "github"
	SourceUpload Source = "upload"
	SourceCLI    Source = "cli"
)

type File struct {
	Project, Path   string
	SHA256, GitSHA  string
	Version         int
	Source          Source
	ThreadID, MsgID string
	UpdatedBy       string
	UpdatedAt       time.Time
	Removed         bool
}

type GHState struct {
	Project          string
	LastCommitSHA    string
	ActivityThreadID string
	LastActivityAt   time.Time
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Added when the bot went multi-server; older databases lack it.
	if _, err := db.Exec(`ALTER TABLE cli_tokens ADD COLUMN guild_id TEXT NOT NULL DEFAULT ''`); err != nil &&
		!strings.Contains(err.Error(), "duplicate column") {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// AdoptLegacy moves rows from before the bot went multi-server, keyed by bare
// project name, onto the home server. It is a no-op once done.
func (s *Store) AdoptLegacy(homeGuild string) error {
	for _, q := range []string{
		`UPDATE files SET project = ? || '/' || project WHERE instr(project, '/') = 0`,
		`UPDATE gh_state SET project = ? || '/' || project WHERE instr(project, '/') = 0`,
		`UPDATE cli_tokens SET guild_id = ? WHERE guild_id = ''`,
	} {
		if _, err := s.db.Exec(q, homeGuild); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

const fileCols = `project, path, sha256, git_sha, version, source, thread_id, message_id, updated_by, updated_at, removed`

func scanFile(row interface{ Scan(...any) error }) (*File, error) {
	var f File
	var at int64
	err := row.Scan(&f.Project, &f.Path, &f.SHA256, &f.GitSHA, &f.Version, &f.Source,
		&f.ThreadID, &f.MsgID, &f.UpdatedBy, &at, &f.Removed)
	if err != nil {
		return nil, err
	}
	f.UpdatedAt = time.Unix(at, 0)
	return &f, nil
}

// GetFile returns nil, nil when the file has never been posted.
func (s *Store) GetFile(project, path string) (*File, error) {
	f, err := scanFile(s.db.QueryRow(`SELECT `+fileCols+` FROM files WHERE project = ? AND path = ?`, project, path))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

func (s *Store) ListFiles(project string) ([]*File, error) {
	rows, err := s.db.Query(`SELECT `+fileCols+` FROM files WHERE project = ? ORDER BY path`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) PutFile(f *File) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO files (`+fileCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		f.Project, f.Path, f.SHA256, f.GitSHA, f.Version, f.Source, f.ThreadID, f.MsgID,
		f.UpdatedBy, f.UpdatedAt.Unix(), f.Removed)
	return err
}

func (s *Store) GetGHState(project string) (*GHState, error) {
	st := &GHState{Project: project}
	var at int64
	err := s.db.QueryRow(`SELECT last_commit_sha, activity_thread_id, last_activity_at FROM gh_state WHERE project = ?`, project).
		Scan(&st.LastCommitSHA, &st.ActivityThreadID, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return st, nil
	}
	if at > 0 {
		st.LastActivityAt = time.Unix(at, 0)
	}
	return st, err
}

func (s *Store) PutGHState(st *GHState) error {
	var at int64
	if !st.LastActivityAt.IsZero() {
		at = st.LastActivityAt.Unix()
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO gh_state (project, last_commit_sha, activity_thread_id, last_activity_at) VALUES (?,?,?,?)`,
		st.Project, st.LastCommitSHA, st.ActivityThreadID, at)
	return err
}

const projectCols = `guild_id, name, forum_channel_id, repo, branch, docs, dev_files`

func scanProject(row interface{ Scan(...any) error }) (*config.Project, error) {
	var p config.Project
	var docs, dev string
	if err := row.Scan(&p.GuildID, &p.Name, &p.ForumChannelID, &p.Repo, &p.Branch, &docs, &dev); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(docs), &p.Docs); err != nil {
		return nil, err
	}
	return &p, json.Unmarshal([]byte(dev), &p.DevFiles)
}

// GetProject returns nil, nil when the server has no such project.
func (s *Store) GetProject(guildID, name string) (*config.Project, error) {
	p, err := scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE guild_id = ? AND name = ?`,
		guildID, config.NormalizeName(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ListProjects returns a server's projects, or every server's when guildID is "".
func (s *Store) ListProjects(guildID string) ([]*config.Project, error) {
	rows, err := s.db.Query(`SELECT `+projectCols+` FROM projects WHERE ? = '' OR guild_id = ? ORDER BY guild_id, name`, guildID, guildID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*config.Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) PutProject(p *config.Project) error {
	docs, _ := json.Marshal(append([]string{}, p.Docs...))
	dev, _ := json.Marshal(append([]string{}, p.DevFiles...))
	_, err := s.db.Exec(`INSERT OR REPLACE INTO projects (`+projectCols+`) VALUES (?,?,?,?,?,?,?)`,
		p.GuildID, p.Name, p.ForumChannelID, p.Repo, p.Branch, docs, dev)
	return err
}

// DeleteProject stops tracking a project. Its file records are kept, so
// re-adding it under the same name picks up where it left off.
func (s *Store) DeleteProject(guildID, name string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE guild_id = ? AND name = ?`, guildID, name)
	return err
}

// DeleteGuild forgets every project in a server the bot was removed from.
func (s *Store) DeleteGuild(guildID string) error {
	_, err := s.db.Exec(`DELETE FROM projects WHERE guild_id = ?`, guildID)
	return err
}

// PutToken stores a CLI token hash for a user, scoped to the server it was
// made in, replacing any previous token.
func (s *Store) PutToken(hash, userID, userName, guildID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cli_tokens WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO cli_tokens (token_hash, user_id, user_name, guild_id, created_at) VALUES (?,?,?,?,?)`,
		hash, userID, userName, guildID, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// LookupToken returns "" when the token hash is unknown.
func (s *Store) LookupToken(hash string) (userName, guildID string, err error) {
	err = s.db.QueryRow(`SELECT user_name, guild_id FROM cli_tokens WHERE token_hash = ?`, hash).Scan(&userName, &guildID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return userName, guildID, err
}
