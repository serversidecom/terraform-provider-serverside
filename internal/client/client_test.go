package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected a Bearer API key, got %q", got)
		}
		if r.Header.Get("X-API-KEY") != "" {
			t.Errorf("the key must be sent in one header only")
		}
		if r.URL.Path != "/v1/networking/virtual-networks/n1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{"success": true, "data": map[string]any{"id": "n1", "name": "backend", "ipBlocks": []any{}}})
	})
	n, err := c.GetNetwork(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != "n1" || n.Name != "backend" {
		t.Fatalf("unexpected network %+v", n)
	}
}

func TestErrorEnvelopeIsDecoded(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]any{
			"success": false, "data": nil, "code": "NETWORK_HAS_ASSIGNED_SERVICE", "message": "Unassign the service first.", "traceId": "abc",
			"details": []map[string]any{{"code": "IN_USE", "message": "attached to 1 service", "field": "id", "location": "route"}},
		})
	})
	err := c.DeleteNetwork(context.Background(), "n1")
	if !HasCode(err, "NETWORK_HAS_ASSIGNED_SERVICE") {
		t.Fatalf("expected code, got %v", err)
	}
	apiErr := err.(*APIError)
	if apiErr.StatusCode != 409 || apiErr.TraceID != "abc" || len(apiErr.Details) != 1 {
		t.Fatalf("unexpected error %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "id: attached to 1 service") {
		t.Fatalf("expected the detail in the message, got %q", err.Error())
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
		if r.URL.Query().Get("pageSize") == "" {
			t.Errorf("expected a pageSize parameter, got %s", r.URL.RawQuery)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		items := []map[string]any{{"id": "k" + strconv.Itoa(page)}}
		writeJSON(w, 200, map[string]any{"success": true, "data": items, "pagination": map[string]any{"page": page, "pageSize": 1, "totalPages": 3, "totalRecords": 3}})
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
	if _, err := c.CreateNetwork(context.Background(), "backend", "PRIVATE", 100); err == nil {
		t.Fatal("expected an error")
	}
	if calls.Load() != 1 {
		t.Fatalf("a POST without an idempotency key must not be retried, got %d calls", calls.Load())
	}
}

func TestIdempotentPostIsRetriedWithSameKey(t *testing.T) {
	var calls atomic.Int32
	var keys []string
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		_ = json.NewDecoder(r.Body).Decode(&body)
		if calls.Add(1) == 1 {
			writeJSON(w, 502, map[string]any{"success": false})
			return
		}
		writeJSON(w, 201, map[string]any{"success": true, "data": map[string]any{"id": "b1", "networkId": "net", "cidr": "198.51.100.0/29"}})
	})
	out, err := c.CreateIPBlock(context.Background(), "plan", "dc", "net", "key-1")
	if err != nil || out.ID != "b1" || out.CIDR != "198.51.100.0/29" {
		t.Fatalf("unexpected result %v %v", out, err)
	}
	if body["plan"] != "plan" || body["datacenter"] != "dc" || body["networkId"] != "net" {
		t.Fatalf("unexpected body %v", body)
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

func TestAttachmentRoutes(t *testing.T) {
	type call struct{ method, path, key string }
	var calls []call
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{r.Method, r.URL.Path, r.Header.Get("Idempotency-Key")})
		if r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		writeJSON(w, 202, map[string]any{"success": true, "data": map[string]any{"status": "APPLYING"}})
	})
	ctx := context.Background()
	if err := c.AttachServer(ctx, "n1", "s1", "seg1", true, "k1"); err != nil {
		t.Fatal(err)
	}
	if err := c.DetachServer(ctx, "n1", "s1", "seg1", "k2"); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{http.MethodPost, "/v1/networking/virtual-networks/n1/attachments", "k1"},
		{http.MethodDelete, "/v1/networking/virtual-networks/n1/attachments/s1/seg1", "k2"},
	}
	if len(calls) != len(want) || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("expected %v, got %v", want, calls)
	}
	if body["serviceId"] != "s1" || body["segmentId"] != "seg1" || body["isNative"] != true {
		t.Fatalf("unexpected attach body %v", body)
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New("", ""); err == nil {
		t.Fatal("expected an error without an API key")
	}
}
