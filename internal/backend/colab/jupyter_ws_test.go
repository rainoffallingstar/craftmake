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

// TestJupyterSignVerify verifies HMAC signing round-trips and rejects tampered
// signatures.
func TestJupyterSignVerify(t *testing.T) {
	key := "secret"
	content := map[string]any{"status": "ok", "execution_count": 1}
	sig := jupyterSign(key, content)
	if sig == "" {
		t.Fatal("expected a signature")
	}
	msg := jupyterMsg{Header: jupyterHeader{MsgType: "execute_reply"}, Content: content, Signature: sig}
	if !jupyterVerify(key, msg) {
		t.Fatal("expected valid signature")
	}
	bad := jupyterMsg{Header: jupyterHeader{MsgType: "execute_reply"}, Content: content, Signature: "deadbeef"}
	if jupyterVerify(key, bad) {
		t.Fatal("expected invalid signature to be rejected")
	}
	if !jupyterVerify("", msg) {
		t.Fatal("empty key should disable verification")
	}
}

// TestJupyterExecutorInterruptIdle verifies Interrupt is a no-op when no
// connection is active.
func TestJupyterExecutorInterruptIdle(t *testing.T) {
	executor := &JupyterWebSocketExecutor{}
	if err := executor.Interrupt(context.Background(), Runtime{}); err != nil {
		t.Fatal(err)
	}
}

// TestJupyterExecutorInterruptActive asserts Interrupt writes a close-control frame
// on the active connection, which the kernel side reads back.
func TestJupyterExecutorInterruptActive(t *testing.T) {
	clientConn, kernel := net.Pipe()
	defer clientConn.Close()
	defer kernel.Close()
	client := &minimalWSConn{conn: clientConn, reader: bufio.NewReader(clientConn)}
	kernelEnd := &minimalWSConn{conn: kernel, reader: bufio.NewReader(kernel)}
	executor := &JupyterWebSocketExecutor{}
	executor.mu.Lock()
	executor.active = client
	executor.mu.Unlock()

	interrupted := make(chan opcodeResult, 1)
	go func() {
		opcode, _, _, err := kernelEnd.readFrame()
		interrupted <- opcodeResult{opcode: opcode, err: err}
	}()
	if err := executor.Interrupt(context.Background(), Runtime{}); err != nil {
		t.Fatal(err)
	}
	result := <-interrupted
	if result.err != nil {
		t.Fatalf("kernel read close frame: %v", result.err)
	}
	if result.opcode != wsControlClose {
		t.Fatalf("expected close control frame, got opcode %d", result.opcode)
	}
}

type opcodeResult struct {
	opcode byte
	err    error
}
