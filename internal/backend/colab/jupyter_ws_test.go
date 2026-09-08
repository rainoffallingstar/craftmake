package colab

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/google/uuid"
)

// TestJupyterStreamAccumulation verifies drainUntilReply accumulates stream text
// before the execute_reply terminator.
func TestJupyterStreamAccumulation(t *testing.T) {
	clientConn, kernel := net.Pipe()
	defer clientConn.Close()
	defer kernel.Close()
	client := &minimalWSConn{conn: clientConn, reader: bufio.NewReader(clientConn)}
	kernelEnd := &minimalWSConn{conn: kernel, reader: bufio.NewReader(kernel)}
	go func() {
		_, _ = kernelEnd.ReadText()
		stream := jupyterMsg{Header: jupyterHeader{MsgID: uuid.NewString(), Session: "s", Username: "u", Date: "t", MsgType: "stream", Version: "5.3"}, Metadata: map[string]any{}, Content: map[string]any{"text": "stdout-line\n"}, Channel: "iopub"}
		sdata, _ := json.Marshal(stream)
		_ = kernelEnd.sendServerText(sdata)
		reply := jupyterMsg{Header: jupyterHeader{MsgID: uuid.NewString(), Session: "s", Username: "u", Date: "t", MsgType: "execute_reply", Version: "5.3"}, Metadata: map[string]any{}, Content: map[string]any{"status": "ok"}, Channel: "shell"}
		rdata, _ := json.Marshal(reply)
		_ = kernelEnd.sendServerText(rdata)
	}()
	executor := &JupyterWebSocketExecutor{SessionID: "sess"}
	msgID, err := executor.sendExecuteRequest(client, "sess", "print('x')")
	if err != nil {
		t.Fatal(err)
	}
	output, err := executor.drainUntilReply(context.Background(), client, msgID)
	if err != nil {
		t.Fatal(err)
	}
	if output != "stdout-line\n" {
		t.Fatalf("stream accumulation = %q, want %q", output, "stdout-line\n")
	}
}
