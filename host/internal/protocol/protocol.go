package protocol

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	ProtocolVersion = 1
	HostVersion     = "0.3.0"
	// maxMessage limits what the host reads from the browser.
	maxMessage = 4 << 20
	// MaxOutgoing is Chrome's limit for one message from a native host.
	MaxOutgoing = 1 << 20
)

var ErrTooLarge = errors.New("native message is too large")

type Incoming struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type Outgoing struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	OK      bool   `json:"ok"`
	Payload any    `json:"payload,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type Handshake struct {
	ProtocolVersion int    `json:"protocolVersion"`
	HostVersion     string `json:"hostVersion"`
	YtDlpVersion    string `json:"ytDlpVersion"`
	FFmpegVersion   string `json:"ffmpegVersion"`
	Deno            bool   `json:"deno"`
	DenoVersion     string `json:"denoVersion"`
	DownloadDir     string `json:"downloadDir"`
	AutoUpdate      bool   `json:"autoUpdate"`
	UpdateWarning   string `json:"updateWarning,omitempty"`
}

type ProgressPayload struct {
	JobID      string   `json:"jobId"`
	Stage      string   `json:"stage"`
	Percent    float64  `json:"percent"`
	BytesDone  int64    `json:"bytesDone"`
	BytesTotal int64    `json:"bytesTotal"`
	SpeedBps   float64  `json:"speedBps"`
	EtaSec     *float64 `json:"etaSec"`
}

func Read(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return nil, err
	}
	if n == 0 {
		return []byte("{}"), nil
	}
	if n > maxMessage {
		return nil, ErrTooLarge
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func Write(w io.Writer, payload []byte) error {
	if len(payload) > MaxOutgoing {
		return ErrTooLarge
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func WriteMessage(w io.Writer, msg Outgoing) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return Write(w, raw)
}

func DecodeIncoming(raw []byte) (Incoming, error) {
	var msg Incoming
	if err := json.Unmarshal(raw, &msg); err != nil {
		return Incoming{}, err
	}
	if msg.Type == "" {
		return Incoming{}, fmt.Errorf("missing message type")
	}
	return msg, nil
}
