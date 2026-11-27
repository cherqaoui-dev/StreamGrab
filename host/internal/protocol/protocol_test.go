package protocol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte(`{"id":"1","type":"HANDSHAKE","payload":{}}`)
	if err := Write(&buf, payload); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %s", got)
	}
}

func TestRejectsOversizedFrame(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, bytes.Repeat([]byte("a"), MaxOutgoing+1)); err != ErrTooLarge {
		t.Fatalf("write error = %v", err)
	}
}

func TestHandshakeFixture(t *testing.T) {
	raw := readFixture(t, "handshake.json")
	msg, err := DecodeIncoming(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Type != "HANDSHAKE" || !json.Valid(raw) {
		t.Fatalf("unexpected fixture: %s", msg.Type)
	}
	var decoded struct {
		OK      bool      `json:"ok"`
		Payload Handshake `json:"payload"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK || decoded.Payload.ProtocolVersion != ProtocolVersion {
		t.Fatalf("handshake payload = %+v", decoded.Payload)
	}
}

func TestProgressFixture(t *testing.T) {
	raw := readFixture(t, "job-progress.json")
	var decoded struct {
		Type    string          `json:"type"`
		Payload ProgressPayload `json:"payload"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "JOB_PROGRESS" || decoded.Payload.JobID != "job-1" || decoded.Payload.Percent != 40 {
		t.Fatalf("progress = %+v", decoded.Payload)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "protocol", "fixtures", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
