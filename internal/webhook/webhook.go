package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"hookrunner/internal/config"
	"hookrunner/internal/workflow"
)

type webhookEvent struct {
	Action  string `json:"action"`
	Comment struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue struct {
		Number int `json:"number"`
	} `json:"issue"`
	PullRequest struct {
		Number int `json:"number"`
	} `json:"pull_request"`
	Repository struct {
		FullName string `json:"full_name"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
}

func Handler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
		if err != nil {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}

		if !VerifySignature(body, r.Header.Get("X-Hub-Signature-256"), cfg.WebhookSecret) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}

		eventType := r.Header.Get("X-GitHub-Event")
		switch eventType {
		case "issue_comment", "pull_request_review_comment":
			// process below
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("event ignored\n"))
			return
		}

		var event webhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if event.Action != "created" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("action ignored\n"))
			return
		}

		if !strings.Contains(event.Comment.Body, "/cc") {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("no /cc trigger\n"))
			return
		}

		prNumber := event.PullRequest.Number
		if prNumber == 0 {
			prNumber = event.Issue.Number
		}

		vars := workflow.TemplateVars{
			RepoFullName:  event.Repository.FullName,
			RepoCloneURL:  event.Repository.CloneURL,
			PRNumber:      fmt.Sprintf("%d", prNumber),
			CommentBody:   event.Comment.Body,
			CommentAuthor: event.Comment.User.Login,
			EventType:     eventType,
		}

		matched := false
		for name, wf := range cfg.Workflows {
			re, err := regexp.Compile(wf.Trigger)
			if err != nil {
				log.Printf("Invalid trigger regex for workflow %q: %v", name, err)
				continue
			}
			if re.MatchString(event.Comment.Body) {
				matched = true
				log.Printf("Workflow %q triggered by %s on %s#%d",
					name, event.Comment.User.Login, event.Repository.FullName, prNumber)
				go workflow.Execute(name, wf, vars)
			}
		}

		if matched {
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte("workflow dispatched\n"))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("no matching workflow\n"))
		}
	}
}

func VerifySignature(payload []byte, signature, secret string) bool {
	if secret == "" {
		return false
	}
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	sig, err := hex.DecodeString(signature[7:])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}
