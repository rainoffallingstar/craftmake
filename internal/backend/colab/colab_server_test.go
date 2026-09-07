package colab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestColabServerClientAssignKeepsAliveRefreshesAndUnassigns(t *testing.T) {
	var xsrfSeen, authSeen string
	colab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen = r.Header.Get(HeaderAuthorization)
		if !strings.HasPrefix(r.URL.Path, TunEndpoint+"/assign") && !strings.HasPrefix(r.URL.Path, TunEndpoint+"/unassign/") && !strings.HasSuffix(r.URL.Path, "/keep-alive/") {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			if strings.Contains(r.URL.Path, "/assign") {
				_ = json.NewEncoder(w).Encode(map[string]any{"acc": "T4", "nbh": "abc", "p": false, "token": "xsrf-1", "variant": "VARIANT_GPU"})
				return
			}
			if strings.Contains(r.URL.Path, "/unassign/") {
				_ = json.NewEncoder(w).Encode(map[string]any{"token": "unassign-xsrf"})
				return
			}
			w.WriteHeader(http.StatusOK) // keep-alive
			return
		case http.MethodPost:
			xsrfSeen = r.Header.Get(HeaderXSRF)
			if strings.Contains(r.URL.Path, "/assign") {
				_ = json.NewEncoder(w).Encode(map[string]any{"accelerator": "T4", "endpoint": "proxy.example.test", "variant": "GPU", "machineShape": 0, "runtimeProxyInfo": map[string]any{"token": "proxy-tok", "tokenExpiresInSeconds": 3600, "url": "https://proxy.example.test"}})
				return
			}
			w.WriteHeader(http.StatusNoContent) // unassign
		}
	}))
	defer colab.Close()
	gapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runtime-proxy-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "fresh", "tokenTtl": "3600s", "url": "https://proxy.example.test"})
			return
		}
		if r.URL.Path == "/v1/user-info" {
			_ = json.NewEncoder(w).Encode(map[string]any{"subscriptionTier": "SUBSCRIPTION_TIER_NONE", "eligibleAccelerators": []any{}, "ineligibleAccelerators": []any{}})
			return
		}
		http.NotFound(w, r)
	}))
	defer gapi.Close()
	client := NewColabServerClient(colab.URL, gapi.URL, colab.Client())
	client.GetAccessToken = func() (string, error) { return "access-tok", nil }
	assignment, err := client.Assign(context.Background(), RuntimeSpec{NotebookHash: "abc", Variant: "GPU", Accelerator: "T4"})
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Endpoint != "proxy.example.test" || authSeen != "Bearer access-tok" || xsrfSeen != "xsrf-1" {
		t.Fatalf("unexpected assign: %#v xsrf=%q auth=%q", assignment, xsrfSeen, authSeen)
	}
	if err := client.KeepAlive(context.Background(), assignment.Endpoint); err != nil {
		t.Fatal(err)
	}
	proxy, err := client.RefreshProxy(context.Background(), assignment.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	if proxy.Token != "fresh" {
		t.Fatalf("unexpected proxy: %#v", proxy)
	}
	if _, err := client.GetUserInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Unassign(context.Background(), assignment.Endpoint); err != nil {
		t.Fatal(err)
	}
}
