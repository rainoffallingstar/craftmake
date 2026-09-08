package colab

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// jupyterExecuteReplyTimeout bounds how long the executor waits for an
// execute_reply after sending each cell.
const jupyterExecuteReplyTimeout = 300 * time.Second

// jupyterMsg is a single Jupyter kernel-protocol message envelope.
type jupyterMsg struct {
	Header       jupyterHeader   `json:"header"`
	ParentHeader json.RawMessage `json:"parent_header"`
	Metadata     map[string]any  `json:"metadata"`
	Content      any             `json:"content"`
	Bufers       []any           `json:"buffers,omitempty"`
	Channel      string          `json:"channel"`
	Signature    string          `json:"signature,omitempty"`
}

type jupyterHeader struct {
	MsgID    string `json:"msg_id"`
	Session  string `json:"session"`
	Username string `json:"username"`
	Date     string `json:"date"`
	MsgType  string `json:"msg_type"`
	Version  string `json:"version"`
}

func newJupyterHeader(session, msgType string) jupyterHeader {
	return jupyterHeader{MsgID: uuid.NewString(), Session: session, Username: "craftmake", Date: time.Now().UTC().Format(time.RFC3339Nano), MsgType: msgType, Version: "5.3"}
}

// jupyterSign computes the Jupyter HMAC-SHA256 signature over the JSON-encoded
// content, hex-encoded. It returns empty when there is no key (unsigned).
func jupyterSign(key string, content any) string {
	if key == "" {
		return ""
	}
	data, err := json.Marshal(content)
	if err != nil {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// jupyterVerify checks an inbound message signature against the shared key. When
// no key is configured signing is considered disabled and returns true.
func jupyterVerify(key string, msg jupyterMsg) bool {
	if key == "" {
		return true
	}
	want := jupyterSign(key, msg.Content)
	if want == "" {
		return false
	}
	return hmac.Equal([]byte(want), []byte(msg.Signature))
}

// JupyterWebSocketExecutor executes a notebook's code cells over the Jupyter
// kernel WebSocket channels endpoint. Runtime.ID is the ws(s) channels URL. It
// accumulates stream/display_data/error output and returns it so the existing
// TaskResult decoder can find the sentinel. HMACKey, when set, is used to sign
// outbound messages and verify inbound ones.
type JupyterWebSocketExecutor struct {
	SessionID string
	Client    *http.Client
	HMACKey   string

	mu     sync.Mutex
	active *minimalWSConn
}

// Interrupt requests the in-flight execution stop by signaling the active
// WebSocket connection. It is a no-op when no execution is active, so it is
// idempotent and safe to call from a cancel path.
func (e *JupyterWebSocketExecutor) Interrupt(_ context.Context, _ Runtime) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.active == nil {
		return nil
	}
	return e.active.Interrupt()
}

func (e *JupyterWebSocketExecutor) ExecuteNotebook(ctx context.Context, runtime Runtime, notebook []byte) (string, error) {
	if runtime.ID == "" {
		return "", fmt.Errorf("Jupyter channels WebSocket URL is required")
	}
	var nb Notebook
	if err := json.Unmarshal(notebook, &nb); err != nil {
		return "", fmt.Errorf("decode notebook: %w", err)
	}
	session := e.SessionID
	if session == "" {
		session = "craftmake"
	}
	conn, err := DialWebSocket(ctx, runtime.ID, e.Client)
	if err != nil {
		return "", classifyDialError(err)
	}
	e.mu.Lock()
	e.active = conn
	e.mu.Unlock()
	defer func() {
		_ = conn.Close()
		e.mu.Lock()
		e.active = nil
		e.mu.Unlock()
	}()
	var output strings.Builder
	for _, cell := range nb.Cells {
		if cell.CellType != "code" || strings.TrimSpace(cell.Source) == "" {
			continue
		}
		msgID, err := e.sendExecuteRequest(conn, session, cell.Source)
		if err != nil {
			return "", &RemoteError{Kind: ErrorKernelDisconnected, Operation: "send execute_request", Err: err}
		}
		cellOutput, err := e.drainUntilReply(ctx, conn, msgID)
		if err != nil {
			return "", err
		}
		output.WriteString(cellOutput)
	}
	return output.String(), nil
}

func (e *JupyterWebSocketExecutor) sendExecuteRequest(conn *minimalWSConn, session, code string) (string, error) {
	msgID := uuid.NewString()
	header := jupyterHeader{MsgID: msgID, Session: session, Username: "craftmake", Date: time.Now().UTC().Format(time.RFC3339Nano), MsgType: "execute_request", Version: "5.3"}
	content := map[string]any{"code": code, "silent": false, "store_history": true, "user_expressions": map[string]any{}, "allow_stdin": false, "stop_on_error": false}
	msg := jupyterMsg{Header: header, ParentHeader: json.RawMessage("{}"), Metadata: map[string]any{}, Content: content, Channel: "shell"}
	msg.Signature = jupyterSign(e.HMACKey, content)
	data, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	if err := conn.WriteText(data); err != nil {
		return "", err
	}
	return msgID, nil
}

// drainUntilReply reads iopub/shell messages until the execute_reply for the
// given cell. It accumulates stream/display_data/error output and terminates on
// execute_reply. Inbound signatures are verified when HMACKey is configured.
func (e *JupyterWebSocketExecutor) drainUntilReply(ctx context.Context, conn *minimalWSConn, msgID string) (string, error) {
	var output strings.Builder
	for {
		select {
		case <-ctx.Done():
			return output.String(), ctx.Err()
		default:
		}
		raw, err := conn.ReadText()
		if err != nil {
			return output.String(), &RemoteError{Kind: ErrorKernelDisconnected, Operation: "read kernel message", Err: err}
		}
		var msg jupyterMsg
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			continue
		}
		if !jupyterVerify(e.HMACKey, msg) {
			continue
		}
		switch msg.Header.MsgType {
		case "stream":
			if content, ok := msg.Content.(map[string]any); ok {
				writeContentText(&output, content, "text")
			}
		case "display_data":
			if content, ok := msg.Content.(map[string]any); ok {
				if data, ok := content["data"].(map[string]any); ok {
					writeContentText(&output, data, "text/plain")
				}
			}
		case "error":
			if content, ok := msg.Content.(map[string]any); ok {
				writeErrorTraceback(&output, content)
			}
		case "execute_reply":
			return output.String(), nil
		}
	}
}

func writeContentText(output *strings.Builder, content map[string]any, key string) {
	if text, ok := content[key].(string); ok {
		output.WriteString(text)
	}
}

func writeErrorTraceback(output *strings.Builder, content map[string]any) {
	if trace, ok := content["traceback"].([]any); ok {
		for _, line := range trace {
			if s, ok := line.(string); ok {
				output.WriteString(s)
				output.WriteString("\n")
			}
		}
	}
}

// classifyDialError maps a handshake failure to a classified RemoteError. An
// HTTP 401/403 during the upgrade indicates expired/unauthorized credentials.
func classifyDialError(err error) error {
	if remote, ok := err.(*RemoteError); ok {
		return remote
	}
	return &RemoteError{Kind: ErrorKernelDisconnected, Operation: "connect Jupyter kernel", Err: err}
}

var _ NotebookExecutor = (*JupyterWebSocketExecutor)(nil)
var _ NotebookInterruptor = (*JupyterWebSocketExecutor)(nil)
