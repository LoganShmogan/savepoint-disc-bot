// Package config loads projects.yaml: bot-wide settings plus any projects for
// the home server. Projects in other servers are made with /project and live
// in the database.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type Config struct {
	GuildID    string    `yaml:"guild_id"`     // home server; the only one that may mirror private repos
	TeamRoleID string    `yaml:"team_role_id"` // home server only; empty = anyone in the server can use commands
	MaxFileMB  int       `yaml:"max_file_mb"`
	PollMins   int       `yaml:"poll_minutes"`
	Projects   []Project `yaml:"projects"`
}

type Project struct {
	GuildID        string   `yaml:"-"` // set from Config.GuildID for YAML projects
	Name           string   `yaml:"name"`
	ForumChannelID string   `yaml:"forum_channel_id"`
	Repo           string   `yaml:"repo"` // "owner/name", optional
	Branch         string   `yaml:"branch"`
	Docs           []string `yaml:"docs"`
	DevFiles       []string `yaml:"dev_files"`
}

// Kinds double as the forum tag names the bot applies to posts.
const (
	KindDocs = "docs"
	KindDev  = "dev"
)

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.MaxFileMB == 0 {
		c.MaxFileMB = 10
	}
	if c.PollMins == 0 {
		c.PollMins = 3
	}
	if len(c.Projects) > 0 && c.GuildID == "" {
		return nil, errors.New("guild_id is required when projects are listed")
	}
	seen := map[string]bool{}
	for i := range c.Projects {
		p := &c.Projects[i]
		p.GuildID = c.GuildID
		if err := p.Normalize(); err != nil {
			return nil, fmt.Errorf("project #%d: %w", i+1, err)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("duplicate project %q", p.Name)
		}
		seen[p.Name] = true
	}
	return &c, nil
}

// Default is the config used when there is no projects.yaml.
func Default() *Config { return &Config{MaxFileMB: 10, PollMins: 3} }

func (c *Config) MaxFileBytes() int { return c.MaxFileMB << 20 }

// NormalizeName lowercases and trims a project name, as stored.
func NormalizeName(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Normalize cleans a project's fields, fills in defaults and validates it.
func (p *Project) Normalize() error {
	p.Name = NormalizeName(p.Name)
	p.Repo = strings.Trim(strings.TrimSpace(p.Repo), "/")
	p.Branch = strings.TrimSpace(p.Branch)
	switch {
	case p.Name == "" || p.ForumChannelID == "":
		return errors.New("name and forum_channel_id are required")
	case len(p.Name) > 50 || strings.ContainsAny(p.Name, "/ "):
		return fmt.Errorf("project %q: names are up to 50 characters, with no spaces or slashes", p.Name)
	case p.Repo != "" && strings.Count(p.Repo, "/") != 1:
		return fmt.Errorf("project %q: repo must be owner/name", p.Name)
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	for _, g := range append(append([]string{}, p.Docs...), p.DevFiles...) {
		if !doublestar.ValidatePattern(g) {
			return fmt.Errorf("project %q: bad glob %q", p.Name, g)
		}
	}
	return nil
}

// Key identifies the project across servers; the store keys its rows by it.
func (p *Project) Key() string { return p.GuildID + "/" + p.Name }

// Match reports which kind of file path is according to the project's globs.
// ok is false when the path matches neither list (GitHub mirroring skips it).
func (p *Project) Match(path string) (kind string, ok bool) {
	for _, g := range p.DevFiles {
		if doublestar.MatchUnvalidated(g, path) {
			return KindDev, true
		}
	}
	for _, g := range p.Docs {
		if doublestar.MatchUnvalidated(g, path) {
			return KindDocs, true
		}
	}
	return KindDocs, false
}

func (p *Project) OwnerRepo() (owner, repo string) {
	owner, repo, _ = strings.Cut(p.Repo, "/")
	return owner, repo
}
