package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestEnvelopeIsUnwrapped(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "test-key" {
			t.Errorf("missing API key header")
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"id": "g1", "name": "web", "rules": []any{}}})
	})
	g, err := c.GetFirewallGroup(context.Background(), "g1")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID != "g1" || g.Name != "web" {
		t.Fatalf("unexpected group %+v", g)
	}
}

func TestErrorEnvelopeIsDecoded(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 422, map[string]any{"success": false, "code": "NETWORK_HAS_ASSIGNED_SERVICE", "message": "Unassign the service first.", "traceId": "abc"})
	})
	err := c.DeleteNetwork(context.Background(), "n1")
	if !HasCode(err, "NETWORK_HAS_ASSIGNED_SERVICE") {
		t.Fatalf("expected code, got %v", err)
	}
	apiErr := err.(*APIError)
	if apiErr.StatusCode != 422 || apiErr.TraceID != "abc" {
		t.Fatalf("unexpected error %+v", apiErr)
	}
}

func TestNonEnvelopeErrorKeepsBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = io.WriteString(w, "SERVICE_NOT_AVAILABLE")
	})
	_, err := c.GetServer(context.Background(), "s1")
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.StatusCode != 403 || apiErr.Message != "SERVICE_NOT_AVAILABLE" {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestNotFound(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"success": false, "code": "NOT_FOUND"})
	})
	_, err := c.GetNetwork(context.Background(), "missing")
	if !IsNotFound(err) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestListAllFollowsPages(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		items := []map[string]any{{"id": "k" + strconv.Itoa(page)}}
		writeJSON(w, 200, map[string]any{"success": true, "data": items, "pagination": map[string]any{"currentPage": page, "totalPages": 3}})
	})
	k, err := c.GetSSHKey(context.Background(), "k3")
	if err != nil || k.ID != "k3" {
		t.Fatalf("expected key k3 from page 3, got %v %v", k, err)
	}
	_, err = c.GetSSHKey(context.Background(), "k9")
	if !IsNotFound(err) {
		t.Fatalf("expected not found for a missing key, got %v", err)
	}
}

func TestGetIsRetriedOn503(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, 503, map[string]any{"success": false, "code": "SERVICE_UNAVAILABLE"})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"id": "n1"}})
	})
	n, err := c.GetNetwork(context.Background(), "n1")
	if err != nil || n.ID != "n1" || calls.Load() != 3 {
		t.Fatalf("expected success on the third call, got %v after %d calls", err, calls.Load())
	}
}

func TestPostWithoutIdempotencyKeyIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeJSON(w, 503, map[string]any{"success": false})
	})
	if _, err := c.CreateFirewallGroup(context.Background(), "web"); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("a POST without an idempotency key must not be retried, got %d calls", calls.Load())
	}
}

func TestIdempotentPostIsRetriedWithSameKey(t *testing.T) {
	var calls atomic.Int32
	var keys []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if calls.Add(1) == 1 {
			writeJSON(w, 502, map[string]any{"success": false})
			return
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"prefixId": "p1"}})
	})
	out, err := c.CreateIPBlock(context.Background(), "plan", "dc", "net", "key-1")
	if err != nil || out.PrefixID != "p1" {
		t.Fatalf("unexpected result %v %v", out, err)
	}
	if len(keys) != 2 || keys[0] != "key-1" || keys[1] != "key-1" {
		t.Fatalf("expected the same key on both attempts, got %v", keys)
	}
}

func TestRuleUpdateSendsWholeRule(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"id": "r1"}})
	})
	port := int64(22)
	_, err := c.UpdateFirewallRule(context.Background(), "g1", "r1", FirewallRuleSpec{Type: "BLOCK", Direction: "INBOUND", Protocol: "TCP", DestPortStart: &port, DestPortEnd: &port}, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"type", "direction", "protocol", "destPortStart", "destPortEnd", "isEnabled"} {
		if _, ok := body[k]; !ok {
			t.Errorf("PATCH body is missing %q: %v", k, body)
		}
	}
	if body["type"] != "BLOCK" || body["isEnabled"] != false {
		t.Errorf("unexpected body %v", body)
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New("", ""); err == nil {
		t.Fatal("expected an error without an API key")
	}
}
