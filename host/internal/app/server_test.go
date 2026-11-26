package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"streamgrab/host/internal/jobs"
	"streamgrab/host/internal/media"
	"streamgrab/host/internal/protocol"
)

type memDisk struct{}

func (memDisk) FreeBytes(string) (uint64, error) { return 1 << 40, nil }

type quietSuspend struct{}

func (quietSuspend) Suspend(int) error { return nil }
func (quietSuspend) Resume(int) error  { return nil }

func TestHandshakeAndRejectsForeignURL(t *testing.T) {
	var in, out bytes.Buffer
	manager := jobs.New(context.Background(), idleRunner{}, quietSuspend{}, memDisk{}, t.TempDir(), "")
	server := &Server{
		In:   &in,
		Out:  &out,
		Jobs: manager,
		Info: func(context.Context, string) (media.Probe, error) {
			return media.Probe{}, nil
		},
		Versions: protocol.Handshake{ProtocolVersion: 1, HostVersion: "0.1.0", DownloadDir: t.TempDir()},
	}
	write(&in, protocol.Incoming{ID: "1", Type: "HANDSHAKE"})
	raw, _ := protocol.Read(&in)
	msg, _ := protocol.DecodeIncoming(raw)
	server.dispatch(context.Background(), msg)
	response := readOut(t, &out)
	if response.Type != "HANDSHAKE" || !response.OK {
		t.Fatalf("%+v", response)
	}

	body, _ := json.Marshal(map[string]string{"url": "https://example.com/watch?v=abcdefghijk"})
	server.dispatch(context.Background(), protocol.Incoming{ID: "2", Type: "GET_INFO", Payload: body})
	rejected := readOut(t, &out)
	if rejected.OK || rejected.Error == nil || rejected.Error.Code != "UNKNOWN" {
		t.Fatalf("%+v", rejected)
	}
}

type idleRunner struct{}

func (idleRunner) Start(context.Context, jobs.Job, func(media.Progress)) (jobs.Handle, error) {
	return nil, &protocol.Error{Code: "UNKNOWN", Message: "not used"}
}

func write(buf *bytes.Buffer, msg protocol.Incoming) {
	raw, _ := json.Marshal(msg)
	_ = protocol.Write(buf, raw)
}

func readOut(t *testing.T, buf *bytes.Buffer) protocol.Outgoing {
	t.Helper()
	raw, err := protocol.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	var msg protocol.Outgoing
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}
