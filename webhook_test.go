package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func computeHMAC(payload, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"created"}`)

	t.Run("valid signature", func(t *testing.T) {
		sig := computeHMAC(string(payload), secret)
		if !verifySignature(payload, sig, secret) {
			t.Error("expected valid signature")
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		if verifySignature(payload, "sha256=deadbeef", secret) {
			t.Error("expected invalid signature")
		}
	})

	t.Run("missing prefix", func(t *testing.T) {
		if verifySignature(payload, "deadbeef", secret) {
			t.Error("expected rejection without sha256= prefix")
		}
	})

	t.Run("empty secret", func(t *testing.T) {
		if verifySignature(payload, "sha256=abc", "") {
			t.Error("expected rejection with empty secret")
		}
	})
}

func makeTestPayload(action, body, repo string, prNum int) string {
	event := map[string]interface{}{
		"action": action,
		"comment": map[string]interface{}{
			"body": body,
			"user": map[string]interface{}{
				"login": "testuser",
			},
		},
		"issue": map[string]interface{}{
			"number": prNum,
		},
		"repository": map[string]interface{}{
			"full_name": repo,
			"clone_url": "https://github.com/" + repo + ".git",
		},
	}
	data, _ := json.Marshal(event)
	return string(data)
}

func TestWebhookHandler(t *testing.T) {
	secret := "test-secret"
	cfg := &Config{
		WebhookSecret: secret,
		Port:          7890,
		Workflows: map[string]WorkflowConfig{
			"test": {
				Trigger: `/cc\s+@claude`,
				Command: "echo test",
				Timeout: 5,
			},
		},
	}

	srv := newServer(cfg)

	t.Run("rejects GET", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/webhook", nil)
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", w.Code)
		}
	})

	t.Run("rejects invalid signature", func(t *testing.T) {
		body := `{"action":"created"}`
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", "sha256=invalid")
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

	t.Run("ignores non-comment events", func(t *testing.T) {
		body := `{"action":"opened"}`
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "push")
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("ignores edited comments", func(t *testing.T) {
		body := makeTestPayload("edited", "/cc @claude", "org/repo", 1)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "action ignored") {
			t.Errorf("expected 'action ignored', got %q", w.Body.String())
		}
	})

	t.Run("ignores comments without /cc", func(t *testing.T) {
		body := makeTestPayload("created", "looks good to me", "org/repo", 1)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no /cc trigger") {
			t.Errorf("expected 'no /cc trigger', got %q", w.Body.String())
		}
	})

	t.Run("dispatches matching workflow", func(t *testing.T) {
		body := makeTestPayload("created", "/cc @claude please review", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusAccepted {
			t.Errorf("expected 202, got %d", w.Code)
		}
	})

	t.Run("no matching workflow", func(t *testing.T) {
		body := makeTestPayload("created", "/cc @someone-else", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		srv.handleWebhook(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no matching workflow") {
			t.Errorf("expected 'no matching workflow', got %q", w.Body.String())
		}
	})
}
