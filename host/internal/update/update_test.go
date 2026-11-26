package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSums(t *testing.T) {
	text := "abc\n0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef *yt-dlp.exe\n"
	got, err := ParseSums(text, "yt-dlp.exe")
	if err != nil || !strings.HasPrefix(got, "0123") {
		t.Fatalf("sum = %s %v", got, err)
	}
	if _, err := ParseSums(text, "missing"); err == nil {
		t.Fatal("expected a missing asset to fail")
	}
}

func TestRejectsNonHTTPS(t *testing.T) {
	err := guardURL("http://github.com/yt-dlp/yt-dlp/releases/download/yt-dlp.exe", OfficialHost)
	if err == nil {
		t.Fatal("http was accepted")
	}
	err = guardURL("https://example.com/yt-dlp.exe", OfficialHost)
	if err == nil {
		t.Fatal("unexpected host was accepted")
	}
}

func TestInstallVerifiesChecksum(t *testing.T) {
	payload := []byte("yt-dlp-binary")
	sum := sha256.Sum256(payload)
	good := hex.EncodeToString(sum[:])
	var assetHits int
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "SHA2-256SUMS"):
			_, _ = w.Write([]byte(good + "  yt-dlp.exe\n" + good + "  yt-dlp\n"))
		default:
			assetHits++
			if assetHits == 1 {
				_, _ = w.Write([]byte("tampered"))
				return
			}
			_, _ = w.Write(payload)
		}
	}))
	defer server.Close()
	client := Client{
		HTTP:     server.Client(),
		SumsURL:  server.URL + "/SHA2-256SUMS",
		AssetURL: server.URL + "/" + AssetName(),
		Allow:    func(string) bool { return true },
		VersionOf: func(string) string {
			return "test"
		},
	}
	dir := t.TempDir()
	if _, err := client.Install(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("tampered download error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, AssetName())); !os.IsNotExist(err) {
		t.Fatal("tampered file was installed")
	}
	got, err := client.Install(context.Background(), dir)
	if err != nil || !got.Updated || got.Version != "test" {
		t.Fatalf("install = %+v %v", got, err)
	}
	again, err := client.Install(context.Background(), dir)
	if err != nil || again.Updated {
		t.Fatalf("second install = %+v %v", again, err)
	}
}
