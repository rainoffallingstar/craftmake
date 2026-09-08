package colab

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestLoopbackServerCapturesCodeAndValidatesState(t *testing.T) {
	server, redirectURI, err := StartLoopbackServer("nonce=abc")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	resultCh := make(chan LoopbackResult, 1)
	go func() {
		result, _ := server.Wait(context.Background(), 5*time.Second)
		resultCh <- result
	}()

	// Simulate the browser callback with the correct state.
	resp, err := http.Get(redirectURI + "?code=the-code&state=nonce%3Dabc")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	result := <-resultCh
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if result.Code != "the-code" {
		t.Fatalf("code = %q", result.Code)
	}
}

func TestLoopbackServerRejectsWrongState(t *testing.T) {
	server, redirectURI, err := StartLoopbackServer("nonce=abc")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	resultCh := make(chan LoopbackResult, 1)
	go func() {
		result, _ := server.Wait(context.Background(), 5*time.Second)
		resultCh <- result
	}()

	resp, err := http.Get(redirectURI + "?code=the-code&state=nonce%3Dwrong")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	result := <-resultCh
	if result.Err == nil {
		t.Fatal("expected state mismatch error")
	}
}

func TestLoopbackServerTimesOut(t *testing.T) {
	server, _, err := StartLoopbackServer("nonce=abc")
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	_, err = server.Wait(context.Background(), 50*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
}
