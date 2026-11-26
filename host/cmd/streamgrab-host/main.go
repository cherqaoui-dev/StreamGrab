package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"streamgrab/host/internal/app"
	"streamgrab/host/internal/jobs"
	"streamgrab/host/internal/platform"
	"streamgrab/host/internal/settings"
	"streamgrab/host/internal/update"
	"streamgrab/host/internal/ytdlp"
)

func main() {
	log.SetOutput(os.Stderr)
	fallback, err := defaultDownloadDir()
	if err != nil {
		log.Fatal(err)
	}
	store := jobsPath()
	downloadDir := loadDownloadDir(store, fallback)
	if err := ytdlp.MustDir(downloadDir); err != nil {
		log.Fatal(err)
	}
	shared := downloadDir
	config := settings.Load(settings.PathBeside(store))
	core := coreDir()
	runner := &ytdlp.Runner{
		YtDlpPath:   ytdlp.PreferredBinary(core),
		FFmpegPath:  ytdlp.ResolveTool("ffmpeg"),
		Dir:         &shared,
		DownloadDir: downloadDir,
	}
	if runner.YtDlpPath == "" {
		log.Print("yt-dlp was not found")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := jobs.New(ctx, runner, platform.NewSuspender(), platform.Disk{}, downloadDir, store)
	manager.OnDownloadDir(func(path string) { shared = path })
	versions := app.NewVersions(downloadDir, runner.Probe(ctx))
	versions.AutoUpdate = settings.Enabled(config)
	versions.UpdateWarning = config.CoreWarning
	server := &app.Server{
		In:           os.Stdin,
		Out:          os.Stdout,
		Jobs:         manager,
		Info:         runner.Info,
		Runner:       runner,
		Versions:     versions,
		CoreDir:      core,
		SettingsPath: settings.PathBeside(store),
		AutoUpdate:   versions.AutoUpdate,
		Updater: update.Client{VersionOf: func(path string) string {
			return ytdlp.CommandVersion(ctx, path)
		}},
	}
	if err := server.Serve(ctx); err != nil && err != io.EOF {
		log.Fatal(err)
	}
}

func defaultDownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads", "StreamGrab"), nil
}

func loadDownloadDir(store, fallback string) string {
	if store == "" {
		return fallback
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(store), "settings.json"))
	if err != nil {
		return fallback
	}
	var body struct {
		DownloadDir string `json:"downloadDir"`
	}
	if json.Unmarshal(raw, &body) != nil || strings.TrimSpace(body.DownloadDir) == "" {
		return fallback
	}
	return body.DownloadDir
}

func coreDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return ""
		}
	}
	return filepath.Join(base, "StreamGrab")
}

func jobsPath() string {
	config, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(config, "StreamGrab", "jobs.json")
}
