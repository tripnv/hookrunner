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
	Review struct {
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"review"`
	PullRequest struct {
		Number int  `json:"number"`
		Merged bool `json:"merged"`
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

		var event webhookEvent
		if err := json.Unmarshal(body, &event); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		// Build the string that triggers are matched against.
		// For comment events: the comment body.
		// For pull_request events: "closed:merged" or "closed:unmerged", etc.
		var matchString string
		var commentBody, commentAuthor string
		switch eventType {
		case "issue_comment", "pull_request_review_comment":
			if event.Action != "created" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("action ignored\n"))
				return
			}
			commentBody = event.Comment.Body
			commentAuthor = event.Comment.User.Login
		case "pull_request_review":
			if event.Action != "submitted" {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("action ignored\n"))
				return
			}
			commentBody = event.Review.Body
			commentAuthor = event.Review.User.Login
		case "pull_request":
			merged := "unmerged"
			if event.PullRequest.Merged {
				merged = "merged"
			}
			matchString = event.Action + ":" + merged
		default:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("event ignored\n"))
			return
		}

		if matchString == "" {
			matchString = commentBody
		}

		prNumber := event.PullRequest.Number
		if prNumber == 0 {
			prNumber = event.Issue.Number
		}

		vars := workflow.TemplateVars{
			RepoFullName:  event.Repository.FullName,
			RepoCloneURL:  event.Repository.CloneURL,
			PRNumber:      fmt.Sprintf("%d", prNumber),
			CommentBody:   commentBody,
			CommentAuthor: commentAuthor,
			EventType:     eventType,
		}

		matched := false
		for name, wf := range cfg.Workflows {
			if !eventMatches(eventType, wf.Events) {
				continue
			}
			if len(wf.Authors) > 0 && !authorAllowed(commentAuthor, wf.Authors) {
				continue
			}
			re, err := regexp.Compile(wf.Trigger)
			if err != nil {
				log.Printf("Invalid trigger regex for workflow %q: %v", name, err)
				continue
			}
			if re.MatchString(matchString) {
				matched = true
				log.Printf("Workflow %q triggered by event %s on %s#%d",
					name, eventType, event.Repository.FullName, prNumber)
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

func eventMatches(eventType string, events []string) bool {
	for _, e := range events {
		if e == eventType {
			return true
		}
	}
	return false
}

func authorAllowed(author string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(a, author) {
			return true
		}
	}
	return false
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
