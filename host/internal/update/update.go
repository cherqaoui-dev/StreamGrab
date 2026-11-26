package update

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	SumsURL  = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/SHA2-256SUMS"
	AssetURL = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/"
	maxBytes = 40 << 20
)

type Client struct {
	HTTP      *http.Client
	SumsURL   string
	AssetURL  string
	Allow     func(host string) bool
	VersionOf func(path string) string
}

type Result struct {
	Updated bool
	Version string
	Path    string
}

func AssetName() string {
	if runtime.GOOS == "windows" {
		return "yt-dlp.exe"
	}
	return "yt-dlp"
}

func OfficialHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	default:
		return false
	}
}

func ParseSums(text, asset string) (string, error) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if name != asset {
			continue
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != 64 || !hexOnly(sum) {
			return "", fmt.Errorf("the official checksum for %s is unreadable", asset)
		}
		return sum, nil
	}
	return "", fmt.Errorf("the official checksum list has no entry for %s", asset)
}

func (c Client) Install(ctx context.Context, dir string) (Result, error) {
	if c.SumsURL == "" {
		c.SumsURL = SumsURL
	}
	if c.AssetURL == "" {
		c.AssetURL = AssetURL + AssetName()
	}
	if err := guardURL(c.SumsURL, c.allow()); err != nil {
		return Result{}, err
	}
	if err := guardURL(c.AssetURL, c.allow()); err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, kept(err)
	}
	asset := AssetName()
	dest := filepath.Join(dir, asset)
	sums, err := c.get(ctx, c.SumsURL)
	if err != nil {
		return Result{}, kept(err)
	}
	want, err := ParseSums(string(sums), asset)
	if err != nil {
		return Result{}, kept(err)
	}
	if match, _ := fileMatches(dest, want); match {
		return Result{Updated: false, Version: c.version(dest), Path: dest}, nil
	}
	body, err := c.get(ctx, c.AssetURL)
	if err != nil {
		return Result{}, kept(err)
	}
	got := sha256.Sum256(body)
	expected, err := hex.DecodeString(want)
	if err != nil || subtle.ConstantTimeCompare(got[:], expected) != 1 {
		return Result{}, errors.New("The yt-dlp download failed the checksum check. The installed version was kept.")
	}
	if err := replaceFile(dest, body); err != nil {
		return Result{}, kept(err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dest, 0o755)
	}
	return Result{Updated: true, Version: c.version(dest), Path: dest}, nil
}

func (c Client) allow() func(string) bool {
	if c.Allow != nil {
		return c.Allow
	}
	return OfficialHost
}

func (c Client) version(path string) string {
	if c.VersionOf == nil {
		return ""
	}
	return c.VersionOf(path)
}

func (c Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "StreamGrab")
	req.Header.Set("Accept", "application/octet-stream, text/plain, */*")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update server returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBytes {
		return nil, errors.New("the yt-dlp download was larger than expected")
	}
	return body, nil
}

func (c Client) http() *http.Client {
	base := c.HTTP
	if base == nil {
		base = &http.Client{Timeout: 2 * time.Minute}
	}
	clone := *base
	allow := c.allow()
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return guardURL(req.URL.String(), allow)
	}
	return &clone
}

func guardURL(raw string, allow func(string) bool) error {
	if !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return errors.New("yt-dlp updates must use https")
	}
	host := raw
	if i := strings.Index(raw, "://"); i >= 0 {
		host = raw[i+3:]
	}
	if slash := strings.IndexAny(host, "/?#"); slash >= 0 {
		host = host[:slash]
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if colon := strings.LastIndex(host, ":"); colon >= 0 {
		host = host[:colon]
	}
	if !allow(host) {
		return errors.New("yt-dlp updates only come from the official release")
	}
	return nil
}

func fileMatches(path, hexSum string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(raw)
	expected, err := hex.DecodeString(hexSum)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(sum[:], expected) == 1, nil
}

func replaceFile(dest string, body []byte) error {
	temp := dest + ".download"
	if err := os.WriteFile(temp, body, 0o644); err != nil {
		return err
	}
	backup := dest + ".prev"
	_ = os.Remove(backup)
	if err := os.Rename(dest, backup); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, dest); err != nil {
		_ = os.Rename(backup, dest)
		_ = os.Remove(temp)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func kept(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	if strings.Contains(text, "installed version was kept") {
		return err
	}
	return fmt.Errorf("yt-dlp couldn't be updated. The installed version was kept. %s", text)
}

func hexOnly(value string) bool {
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
