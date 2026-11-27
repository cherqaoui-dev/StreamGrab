package main

import (
	"context"
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
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		log.Fatalf("create download directory: %v", err)
	}
	config := settings.Load(settings.PathBeside(store))
	core := coreDir()
	runner := &ytdlp.Runner{
		YtDlpPath:   ytdlp.PreferredBinary(core),
		FFmpegPath:  ytdlp.ResolveTool("ffmpeg"),
		DownloadDir: downloadDir,
	}
	if runner.YtDlpPath == "" {
		log.Print("yt-dlp was not found")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	manager := jobs.New(ctx, runner, platform.NewSuspender(), platform.Disk{}, downloadDir, store)
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
	err = server.Serve(ctx)
	// The browser has gone, so downloads stop here and resume next time.
	manager.Shutdown()
	if err != nil && err != io.EOF {
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
	if dir := settings.Load(settings.PathBeside(store)).DownloadDir; strings.TrimSpace(dir) != "" {
		return dir
	}
	return fallback
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
