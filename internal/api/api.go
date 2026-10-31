// Package api is the small HTTP API the docsync CLI talks to. It is meant to
// be reachable over Tailscale only.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

type Server struct {
	Store *store.Store
	Sync  *filesync.Syncer
}

// Wire types, shared with the CLI.
type CheckFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type CheckResult struct {
	Path   string          `json:"path"` // cleaned path to upload under
	Status filesync.Status `json:"status"`
}

type UploadResult struct {
	Status  filesync.Status `json:"status"`
	Version int             `json:"version"`
	URL     string          `json:"url"`
}

// caller is who a token belongs to and the server it was made in.
type caller struct{ User, GuildID string }

type callerKey struct{}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/projects", s.projects)
	mux.HandleFunc("POST /api/projects/{project}/check", s.check)
	mux.HandleFunc("PUT /api/projects/{project}/files/{path...}", s.upload)
	return s.auth(mux)
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || tok == "" {
			httpError(w, http.StatusUnauthorized, "missing token; run /token in Discord")
			return
		}
		user, guild, err := s.Store.LookupToken(filesync.Hash([]byte(tok)))
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if user == "" {
			httpError(w, http.StatusUnauthorized, "unknown token; run /token in Discord for a new one")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, caller{user, guild})))
	})
}

func callerOf(r *http.Request) caller { return r.Context().Value(callerKey{}).(caller) }

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Store.ListProjects(callerOf(r).GuildID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := []string{}
	for _, p := range projects {
		names = append(names, p.Name)
	}
	writeJSON(w, names)
}

// project looks the name up in the caller's server only.
func (s *Server) project(w http.ResponseWriter, r *http.Request) (*config.Project, bool) {
	p, err := s.Store.GetProject(callerOf(r).GuildID, r.PathValue("project"))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if p == nil {
		httpError(w, http.StatusNotFound, "no such project: "+r.PathValue("project"))
	}
	return p, p != nil
}

func (s *Server) check(w http.ResponseWriter, r *http.Request) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	var files []CheckFile
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&files); err != nil {
		httpError(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return
	}
	out := make([]CheckResult, 0, len(files))
	for _, f := range files {
		clean, err := filesync.CleanPath(f.Path)
		if err != nil {
			httpError(w, http.StatusBadRequest, f.Path+": "+err.Error())
			return
		}
		st, err := s.Sync.Check(p, clean, f.SHA256)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, CheckResult{Path: clean, Status: st})
	}
	writeJSON(w, out)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	p, ok := s.project(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(s.Sync.MaxBytes)))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		respond(w, http.StatusRequestEntityTooLarge, UploadResult{Status: filesync.TooLarge})
		return
	}
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := s.Sync.Upsert(r.Context(), filesync.Request{
		Project: p, Path: r.PathValue("path"), Content: body, Source: store.SourceCLI,
		User: callerOf(r).User, Force: r.URL.Query().Get("force") == "1",
	})
	if err != nil {
		slog.Error("cli upload failed", "project", p.Name, "path", r.PathValue("path"), "err", err)
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := UploadResult{Status: res.Status, Version: res.Version}
	if res.ThreadID != "" {
		out.URL = filesync.PostURL(p.GuildID, res.ThreadID)
	}
	writeJSON(w, out)
}

func writeJSON(w http.ResponseWriter, v any) { respond(w, http.StatusOK, v) }

func httpError(w http.ResponseWriter, code int, msg string) {
	respond(w, code, map[string]string{"error": msg})
}

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
