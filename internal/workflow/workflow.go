package workflow

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
	"time"

	"hookrunner/internal/config"
)

type TemplateVars struct {
	RepoFullName  string
	RepoCloneURL  string
	PRNumber      string
	CommentBody   string
	CommentAuthor string
	EventType     string
}

var shellMetaChars = regexp.MustCompile(`[;&|$` + "`" + `\\!(){}\[\]<>*?~#'"\n\r]`)

func Sanitize(input string) string {
	return shellMetaChars.ReplaceAllString(input, "")
}

func SanitizeVars(vars TemplateVars) TemplateVars {
	return TemplateVars{
		RepoFullName:  Sanitize(vars.RepoFullName),
		RepoCloneURL:  Sanitize(vars.RepoCloneURL),
		PRNumber:      Sanitize(vars.PRNumber),
		CommentBody:   Sanitize(vars.CommentBody),
		CommentAuthor: Sanitize(vars.CommentAuthor),
		EventType:     Sanitize(vars.EventType),
	}
}

func RenderTemplate(tmpl string, vars TemplateVars) (string, error) {
	t, err := template.New("cmd").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func Execute(name string, wf config.WorkflowConfig, vars TemplateVars) {
	safe := SanitizeVars(vars)

	cmd, err := RenderTemplate(wf.Command, safe)
	if err != nil {
		log.Printf("Workflow %q: template error: %v", name, err)
		return
	}

	workdir := ""
	if wf.Workdir != "" {
		workdir, err = RenderTemplate(wf.Workdir, safe)
		if err != nil {
			log.Printf("Workflow %q: workdir template error: %v", name, err)
			return
		}
		workdir = config.ExpandTilde(workdir)
	}

	timeout := time.Duration(wf.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("────── Workflow %q started ──────", name)
	log.Printf("Command: %s", cmd)

	start := time.Now()

	proc := exec.CommandContext(ctx, "sh", "-c", cmd)
	proc.Env = append(os.Environ(),
		"HR_PR_NUMBER="+vars.PRNumber,
		"HR_REPO="+vars.RepoFullName,
		"HR_COMMENT_BODY="+vars.CommentBody,
		"HR_COMMENT_AUTHOR="+vars.CommentAuthor,
		"HR_EVENT_TYPE="+vars.EventType,
	)
	if workdir != "" {
		proc.Dir = workdir
	}

	output, err := proc.CombinedOutput()
	duration := time.Since(start)
	outStr := strings.TrimSpace(string(output))

	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("Workflow %q: timed out after %ds", name, wf.Timeout)
		log.Printf("────── Workflow %q timed out (%.1fs) ──────", name, duration.Seconds())
		log.Printf("════════════════════════════════════════")
		return
	}

	if err != nil {
		log.Printf("Workflow %q: error: %v\nOutput: %s", name, err, outStr)
		log.Printf("────── Workflow %q failed (%.1fs) ──────", name, duration.Seconds())
		log.Printf("════════════════════════════════════════")
		return
	}

	if outStr != "" {
		log.Printf("Output: %s", outStr)
	}
	log.Printf("────── Workflow %q completed (%.1fs) ──────", name, duration.Seconds())
	log.Printf("════════════════════════════════════════")
}
