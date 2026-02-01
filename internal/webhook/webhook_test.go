package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrunner/internal/config"
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
		if !VerifySignature(payload, sig, secret) {
			t.Error("expected valid signature")
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		if VerifySignature(payload, "sha256=deadbeef", secret) {
			t.Error("expected invalid signature")
		}
	})

	t.Run("missing prefix", func(t *testing.T) {
		if VerifySignature(payload, "deadbeef", secret) {
			t.Error("expected rejection without sha256= prefix")
		}
	})

	t.Run("empty secret", func(t *testing.T) {
		if VerifySignature(payload, "sha256=abc", "") {
			t.Error("expected rejection with empty secret")
		}
	})
}

func makeCommentPayload(action, body, repo string, prNum int) string {
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

func makePRPayload(action, repo string, prNum int, merged bool) string {
	event := map[string]interface{}{
		"action": action,
		"pull_request": map[string]interface{}{
			"number": prNum,
			"merged": merged,
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
	cfg := &config.Config{
		WebhookSecret: secret,
		Port:          7890,
		Workflows: map[string]config.WorkflowConfig{
			"test": {
				Events:  []string{"issue_comment", "pull_request_review_comment", "pull_request_review"},
				Trigger: `/cc\s+@claude`,
				Command: "echo test",
				Timeout: 5,
			},
		},
	}

	handler := Handler(cfg)

	t.Run("rejects GET", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/webhook", nil)
		w := httptest.NewRecorder()
		handler(w, req)
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
		handler(w, req)
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
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("ignores edited comments", func(t *testing.T) {
		body := makeCommentPayload("edited", "/cc @claude", "org/repo", 1)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "action ignored") {
			t.Errorf("expected 'action ignored', got %q", w.Body.String())
		}
	})

	t.Run("ignores comments without trigger", func(t *testing.T) {
		body := makeCommentPayload("created", "looks good to me", "org/repo", 1)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no matching workflow") {
			t.Errorf("expected 'no matching workflow', got %q", w.Body.String())
		}
	})

	t.Run("dispatches matching workflow", func(t *testing.T) {
		body := makeCommentPayload("created", "/cc @claude please review", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusAccepted {
			t.Errorf("expected 202, got %d", w.Code)
		}
	})

	t.Run("no matching workflow", func(t *testing.T) {
		body := makeCommentPayload("created", "/cc @someone-else", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no matching workflow") {
			t.Errorf("expected 'no matching workflow', got %q", w.Body.String())
		}
	})

	t.Run("comment workflow ignores pull_request event", func(t *testing.T) {
		body := makePRPayload("closed", "org/repo", 42, true)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no matching workflow") {
			t.Errorf("expected 'no matching workflow', got %q", w.Body.String())
		}
	})
}

func makeReviewPayload(action, body, repo string, prNum int) string {
	event := map[string]interface{}{
		"action": action,
		"review": map[string]interface{}{
			"body": body,
			"user": map[string]interface{}{
				"login": "reviewer",
			},
		},
		"pull_request": map[string]interface{}{
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

func TestPullRequestReview(t *testing.T) {
	secret := "test-secret"
	cfg := &config.Config{
		WebhookSecret: secret,
		Port:          7890,
		Workflows: map[string]config.WorkflowConfig{
			"test": {
				Events:  []string{"issue_comment", "pull_request_review_comment", "pull_request_review"},
				Trigger: `/cc\s+@claude`,
				Command: "echo test",
				Timeout: 5,
			},
		},
	}

	handler := Handler(cfg)

	t.Run("dispatches on review with /cc", func(t *testing.T) {
		body := makeReviewPayload("submitted", "/cc @claude please review", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request_review")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusAccepted {
			t.Errorf("expected 202, got %d", w.Code)
		}
	})

	t.Run("ignores review without trigger", func(t *testing.T) {
		body := makeReviewPayload("submitted", "looks good", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request_review")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "no matching workflow") {
			t.Errorf("expected 'no matching workflow', got %q", w.Body.String())
		}
	})

	t.Run("ignores non-submitted review action", func(t *testing.T) {
		body := makeReviewPayload("edited", "/cc @claude", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request_review")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestPullRequestMergeWorkflow(t *testing.T) {
	secret := "test-secret"
	cfg := &config.Config{
		WebhookSecret: secret,
		Port:          7890,
		Workflows: map[string]config.WorkflowConfig{
			"cleanup": {
				Events:  []string{"pull_request"},
				Trigger: `^closed:merged$`,
				Command: "echo cleanup",
				Timeout: 5,
			},
		},
	}

	handler := Handler(cfg)

	t.Run("dispatches on merged PR", func(t *testing.T) {
		body := makePRPayload("closed", "org/repo", 42, true)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusAccepted {
			t.Errorf("expected 202, got %d", w.Code)
		}
	})

	t.Run("ignores closed-but-unmerged PR", func(t *testing.T) {
		body := makePRPayload("closed", "org/repo", 42, false)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("ignores opened PR", func(t *testing.T) {
		body := makePRPayload("opened", "org/repo", 42, false)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "pull_request")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("ignores comment events", func(t *testing.T) {
		body := makeCommentPayload("created", "/cc @claude", "org/repo", 42)
		sig := computeHMAC(body, secret)
		req := httptest.NewRequest("POST", "/webhook", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Event", "issue_comment")
		w := httptest.NewRecorder()
		handler(w, req)
		// cleanup workflow only listens to pull_request, so no match
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}
