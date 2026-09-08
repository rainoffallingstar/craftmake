package colab

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"time"
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

// JupyterWebSocketExecutor executes a notebook's code cells over the Jupyter
// kernel WebSocket channels endpoint. Runtime.ID is the ws(s) channels URL. It
// accumulates `stream` output and returns it as the execution output so the
// existing TaskResult decoder can find the sentinel.
type JupyterWebSocketExecutor struct {
	SessionID string
	Client    *http.Client
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
		return "", &RemoteError{Kind: ErrorKernelDisconnected, Operation: "connect Jupyter kernel", Err: err}
	}
	defer conn.Close()
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
	msg := jupyterMsg{
		Header:       jupyterHeader{MsgID: msgID, Session: session, Username: "craftmake", Date: time.Now().UTC().Format(time.RFC3339Nano), MsgType: "execute_request", Version: "5.3"},
		ParentHeader: json.RawMessage("{}"),
		Metadata:     map[string]any{},
		Content:      map[string]any{"code": code, "silent": false, "store_history": true, "user_expressions": map[string]any{}, "allow_stdin": false, "stop_on_error": false},
		Channel:      "shell",
	}
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
// given msg_id. It accumulates stream text and error tracebacks.
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
		switch msg.Header.MsgType {
		case "stream":
			if content, ok := msg.Content.(map[string]any); ok {
				if text, ok := content["text"].(string); ok {
					output.WriteString(text)
				}
			}
		case "error":
			if content, ok := msg.Content.(map[string]any); ok {
				if trace, ok := content["traceback"].([]any); ok {
					for _, line := range trace {
						if s, ok := line.(string); ok {
							output.WriteString(s)
							output.WriteString("\n")
						}
					}
				}
			}
		case "execute_reply":
			return output.String(), nil
		}
	}
}

var _ NotebookExecutor = (*JupyterWebSocketExecutor)(nil)
