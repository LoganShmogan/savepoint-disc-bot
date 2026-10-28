// Package config loads projects.yaml, which maps each project to its Discord
// forum channel and GitHub repo.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type Config struct {
	GuildID    string    `yaml:"guild_id"`
	TeamRoleID string    `yaml:"team_role_id"` // empty = anyone in the server can use commands
	MaxFileMB  int       `yaml:"max_file_mb"`
	PollMins   int       `yaml:"poll_minutes"`
	Projects   []Project `yaml:"projects"`
}

type Project struct {
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
	seen := map[string]bool{}
	for i := range c.Projects {
		p := &c.Projects[i]
		p.Name = strings.ToLower(strings.TrimSpace(p.Name))
		if p.Name == "" || p.ForumChannelID == "" {
			return nil, fmt.Errorf("project #%d: name and forum_channel_id are required", i+1)
		}
		if seen[p.Name] {
			return nil, fmt.Errorf("duplicate project %q", p.Name)
		}
		seen[p.Name] = true
		if p.Repo != "" && !strings.Contains(p.Repo, "/") {
			return nil, fmt.Errorf("project %q: repo must be owner/name", p.Name)
		}
		if p.Branch == "" {
			p.Branch = "main"
		}
		for _, g := range append(append([]string{}, p.Docs...), p.DevFiles...) {
			if !doublestar.ValidatePattern(g) {
				return nil, fmt.Errorf("project %q: bad glob %q", p.Name, g)
			}
		}
	}
	return &c, nil
}

func (c *Config) MaxFileBytes() int { return c.MaxFileMB << 20 }

func (c *Config) Project(name string) (*Project, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for i := range c.Projects {
		if c.Projects[i].Name == name {
			return &c.Projects[i], true
		}
	}
	return nil, false
}

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
