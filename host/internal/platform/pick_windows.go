//go:build windows

package platform

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"unicode/utf16"
)

var ErrCancelled = errors.New("folder picker cancelled")

func PickDirectory() (string, error) {
	file, err := os.CreateTemp("", "streamgrab-pick-*.txt")
	if err != nil {
		return "", err
	}
	name := file.Name()
	_ = file.Close()
	defer os.Remove(name)
	script := `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = 'Choose the StreamGrab download folder'
$dialog.ShowNewFolderButton = $true
if ($dialog.ShowDialog() -ne [System.Windows.Forms.DialogResult]::OK) { exit 0 }
[System.IO.File]::WriteAllText($env:STREAMGRAB_PICK, $dialog.SelectedPath)
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-STA", "-WindowStyle", "Hidden", "-Command", script)
	cmd.Env = append(os.Environ(), "STREAMGRAB_PICK="+name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", errors.New(strings.TrimSpace(decodePowerShell(output)))
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(decodePowerShell(raw))
	if path == "" {
		return "", ErrCancelled
	}
	return path, nil
}

func decodePowerShell(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFF && raw[1] == 0xFE {
		raw = raw[2:]
		return decodeUTF16(raw)
	}
	if looksUTF16(raw) {
		return decodeUTF16(raw)
	}
	return string(raw)
}

func looksUTF16(raw []byte) bool {
	if len(raw) < 4 || len(raw)%2 != 0 {
		return false
	}
	zeros := 0
	for i := 1; i < len(raw); i += 2 {
		if raw[i] == 0 {
			zeros++
		}
	}
	return zeros > len(raw)/4
}

func decodeUTF16(raw []byte) string {
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	return string(utf16.Decode(units))
}
