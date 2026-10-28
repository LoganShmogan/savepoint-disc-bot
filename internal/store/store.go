// Package store persists what the bot has posted, so restarts never repost
// files and changed files can be found by hash.
package store

import (
	"database/sql"
	"errors"
	"time"

	_ "modernc.org/sqlite"
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
	return &Store{db: db}, nil
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

// PutToken stores a CLI token hash for a user, replacing any previous token.
func (s *Store) PutToken(hash, userID, userName string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM cli_tokens WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO cli_tokens (token_hash, user_id, user_name, created_at) VALUES (?,?,?,?)`,
		hash, userID, userName, time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

// LookupToken returns "" when the token hash is unknown.
func (s *Store) LookupToken(hash string) (userName string, err error) {
	err = s.db.QueryRow(`SELECT user_name FROM cli_tokens WHERE token_hash = ?`, hash).Scan(&userName)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return userName, err
}
