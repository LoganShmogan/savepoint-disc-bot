// Command docsync pushes a local folder of docs to a project's Discord forum,
// uploading only files that are new or changed.
//
//	docsync login --server http://your-server:8080 --token phb_...
//	docsync projects
//	docsync push ./docs --project memeline [--prefix docs/] [--dry-run] [--force]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/api"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
)

const usage = `docsync: sync local docs to your team's Discord forums

  docsync login --server URL --token TOKEN   save credentials (get a token with /token in Discord)
  docsync projects                           list projects
  docsync push DIR --project NAME [flags]    upload new/changed files in DIR

push flags:
  --prefix PATH   store files under this path, e.g. docs/ (default: paths relative to DIR)
  --dry-run       show what would happen without uploading
  --force         overwrite files managed by GitHub
`

type creds struct {
	Server string `json:"server"`
	Token  string `json:"token"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "login":
		err = login(os.Args[2:])
	case "projects":
		err = projects()
	case "push":
		err = push(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q (try docsync help)", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// parse allows flags before or after positional args (flag stops at the first one).
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func credsPath() (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "docsync", "config.json"), err
}

func loadCreds() (*creds, error) {
	c := &creds{Server: os.Getenv("DOCSYNC_SERVER"), Token: os.Getenv("DOCSYNC_TOKEN")}
	if c.Server != "" && c.Token != "" {
		return c, nil
	}
	p, err := credsPath()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errors.New("not logged in; run /token in Discord, then docsync login")
	}
	if err != nil {
		return nil, err
	}
	return c, json.Unmarshal(raw, c)
}

func login(args []string) error {
	f := flag.NewFlagSet("login", flag.ExitOnError)
	server := f.String("server", "", "bot API URL, e.g. http://your-server:8080")
	token := f.String("token", "", "token from /token in Discord")
	if _, err := parse(f, args); err != nil {
		return err
	}
	if *server == "" || *token == "" {
		return errors.New("--server and --token are required")
	}
	c := &creds{Server: strings.TrimRight(*server, "/"), Token: *token}
	var names []string
	if err := call(c, "GET", "/api/projects", nil, &names); err != nil {
		return fmt.Errorf("couldn't reach the bot with that token: %w", err)
	}
	p, err := credsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("Logged in. Projects: %s\n", strings.Join(names, ", "))
	return nil
}

func projects() error {
	c, err := loadCreds()
	if err != nil {
		return err
	}
	var names []string
	if err := call(c, "GET", "/api/projects", nil, &names); err != nil {
		return err
	}
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}

func push(args []string) error {
	f := flag.NewFlagSet("push", flag.ExitOnError)
	project := f.String("project", "", "project name")
	prefix := f.String("prefix", "", "path prefix on Discord")
	dry := f.Bool("dry-run", false, "don't upload")
	force := f.Bool("force", false, "overwrite GitHub-managed files")
	pos, err := parse(f, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 || *project == "" {
		return errors.New("usage: docsync push DIR --project NAME")
	}
	c, err := loadCreds()
	if err != nil {
		return err
	}

	local, err := scan(pos[0], *prefix)
	if err != nil {
		return err
	}
	if len(local) == 0 {
		fmt.Println("No files found.")
		return nil
	}
	var check []api.CheckFile
	for p, l := range local {
		check = append(check, api.CheckFile{Path: p, SHA256: l.sum})
	}
	var results []api.CheckResult
	if err := call(c, "POST", "/api/projects/"+url.PathEscape(*project)+"/check", check, &results); err != nil {
		return err
	}

	counts := map[string]int{}
	failed := false
	for i, r := range results {
		file := local[check[i].Path].file
		switch {
		case r.Status == filesync.Unchanged:
			fmt.Printf("  = %s\n", r.Path)
			counts["unchanged"]++
			continue
		case r.Status == filesync.Blocked && !*force:
			fmt.Printf("  🔒 %s (managed by GitHub; push to the repo or use --force)\n", r.Path)
			counts["skipped"]++
			continue
		case *dry:
			fmt.Printf("  %s %s (would upload)\n", map[bool]string{true: "+", false: "~"}[r.Status == filesync.Created], r.Path)
			counts["to upload"]++
			continue
		}
		res, err := upload(c, *project, r.Path, file, *force)
		if err != nil {
			fmt.Printf("  ✗ %s: %v\n", r.Path, err)
			counts["failed"]++
			failed = true
			continue
		}
		switch res.Status {
		case filesync.Created:
			fmt.Printf("  + %s  %s\n", r.Path, res.URL)
		case filesync.Updated:
			fmt.Printf("  ~ %s → v%d  %s\n", r.Path, res.Version, res.URL)
		case filesync.TooLarge:
			fmt.Printf("  ✗ %s: over Discord's size limit\n", r.Path)
			failed = true
		default:
			fmt.Printf("  = %s (%s)\n", r.Path, res.Status)
		}
		counts[string(res.Status)]++
	}
	var summary []string
	for k, v := range counts {
		summary = append(summary, fmt.Sprintf("%d %s", v, k))
	}
	fmt.Println(strings.Join(summary, ", "))
	if failed {
		return errors.New("some files failed")
	}
	return nil
}

type localFile struct{ file, sum string }

// scan hashes every file under dir, skipping dotfiles and dependency folders.
func scan(dir, prefix string) (map[string]localFile, error) {
	out := map[string]localFile{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if p != dir && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		key, err := filesync.CleanPath(prefix + "/" + filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		out[key] = localFile{file: p, sum: filesync.Hash(raw)}
		return nil
	})
	return out, err
}

func upload(c *creds, project, path, file string, force bool) (*api.UploadResult, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := "/api/projects/" + url.PathEscape(project) + "/files/" + strings.Join(segs, "/")
	if force {
		u += "?force=1"
	}
	var res api.UploadResult
	return &res, call(c, "PUT", u, raw, &res)
}

var client = &http.Client{Timeout: 2 * time.Minute}

// call sends body as JSON (or raw bytes for []byte) and decodes the reply into out.
func call(c *creds, method, path string, body, out any) error {
	var rd io.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, c.Server+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusRequestEntityTooLarge {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(raw, out)
}
