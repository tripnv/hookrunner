package main

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"text/template"
	"time"
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

func sanitize(input string) string {
	return shellMetaChars.ReplaceAllString(input, "")
}

func sanitizeVars(vars TemplateVars) TemplateVars {
	return TemplateVars{
		RepoFullName:  sanitize(vars.RepoFullName),
		RepoCloneURL:  sanitize(vars.RepoCloneURL),
		PRNumber:      sanitize(vars.PRNumber),
		CommentBody:   sanitize(vars.CommentBody),
		CommentAuthor: sanitize(vars.CommentAuthor),
		EventType:     sanitize(vars.EventType),
	}
}

func renderTemplate(tmpl string, vars TemplateVars) (string, error) {
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

func executeWorkflow(name string, wf WorkflowConfig, vars TemplateVars) {
	safe := sanitizeVars(vars)

	cmd, err := renderTemplate(wf.Command, safe)
	if err != nil {
		log.Printf("Workflow %q: template error: %v", name, err)
		return
	}

	workdir := ""
	if wf.Workdir != "" {
		workdir, err = renderTemplate(wf.Workdir, safe)
		if err != nil {
			log.Printf("Workflow %q: workdir template error: %v", name, err)
			return
		}
		workdir = expandTilde(workdir)
	}

	timeout := time.Duration(wf.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Printf("Workflow %q: executing: %s", name, cmd)

	proc := exec.CommandContext(ctx, "sh", "-c", cmd)
	if workdir != "" {
		proc.Dir = workdir
	}

	output, err := proc.CombinedOutput()
	outStr := strings.TrimSpace(string(output))

	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("Workflow %q: timed out after %ds", name, wf.Timeout)
		return
	}

	if err != nil {
		log.Printf("Workflow %q: error: %v\nOutput: %s", name, err, outStr)
		return
	}

	log.Printf("Workflow %q: completed successfully\nOutput: %s", name, outStr)
}
