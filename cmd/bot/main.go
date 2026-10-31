// Command bot runs the Discord bot, the GitHub watcher and the docsync API.
package main

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/LoganShmogan/savepoint-disc-bot/internal/api"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/config"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/discord"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/filesync"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/ghwatch"
	"github.com/LoganShmogan/savepoint-disc-bot/internal/store"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return errors.New("DISCORD_TOKEN is not set")
	}
	cfgPath := env("CONFIG_PATH", "projects.yaml")
	cfg, err := config.Load(cfgPath)
	if errors.Is(err, fs.ErrNotExist) {
		slog.Warn("no config file; running with defaults and no home server", "path", cfgPath)
		cfg, err = config.Default(), nil
	}
	if err != nil {
		return err
	}
	dbPath := env("DB_PATH", "data/hub.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if cfg.GuildID != "" {
		if err := st.AdoptLegacy(cfg.GuildID); err != nil {
			return err
		}
	}
	// projects.yaml wins over /project edits for the projects it lists.
	for i := range cfg.Projects {
		if err := st.PutProject(&cfg.Projects[i]); err != nil {
			return err
		}
	}

	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return err
	}
	s.Identify.Intents = discordgo.IntentsGuilds

	poster := discord.NewPoster(s)
	syncer := &filesync.Syncer{Store: st, Poster: poster, MaxBytes: cfg.MaxFileBytes()}
	bot := discord.NewBot(s, cfg, st, syncer)
	bot.APIURL = env("PUBLIC_API_URL", "http://localhost:8080")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var watcher *ghwatch.Watcher
	if gh := os.Getenv("GITHUB_TOKEN"); gh != "" {
		watcher = ghwatch.New(gh, cfg, st, syncer, poster)
		bot.Trigger, bot.CheckRepo = watcher.Trigger, watcher.CheckRepo
	} else {
		slog.Warn("GITHUB_TOKEN not set; GitHub sync disabled")
	}

	if err := s.Open(); err != nil {
		return err
	}
	defer s.Close()
	if err := bot.Register(); err != nil {
		return err
	}
	slog.Info("connected to Discord", "user", s.State.User.Username, "servers", len(s.State.Guilds))
	if watcher != nil {
		go watcher.Run(ctx)
	}

	srv := &http.Server{Addr: env("API_ADDR", ":8080"), Handler: (&api.Server{Store: st, Sync: syncer}).Handler()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server", "err", err)
			stop()
		}
	}()
	slog.Info("docsync API listening", "addr", srv.Addr)

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
