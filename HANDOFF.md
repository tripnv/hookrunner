# hookrunner -- Handoff Document

## What is hookrunner?

hookrunner is a lightweight local GitHub webhook server written in Go. It listens for incoming GitHub webhook events and dispatches local shell workflows in response to specific triggers -- most notably `/cc @<name>` mentions in pull request comments and reviews.

The primary use case is enabling local automation: for example, triggering a workflow when someone comments `/cc` on a PR.

---

## Architecture

```
GitHub Webhook POST
        |
        v
  HTTP Server (127.0.0.1:8443)
        |
        v
  Signature Verification (HMAC-SHA256)
        |
        v
  Event Parsing (issue_comment, pull_request_review, etc.)
        |
        v
  Filtering Pipeline:
    1. Event type filter
    2. Action filter (created/submitted only)
    3. Author filter (optional allowlist)
    4. Trigger regex match
        |
        v
  Workflow Execution (async, via sh -c)
```

### Module Structure

| Package | Responsibility |
|---|---|
| `cmd/hookrunner` | CLI entry point, flag parsing, signal handling |
| `internal/config` | YAML config loading, validation, defaults |
| `internal/server` | HTTP server setup, routing, graceful shutdown |
| `internal/webhook` | Webhook parsing, signature verification, event routing |
| `internal/workflow` | Template rendering, input sanitization, command execution |
| `internal/daemon` | Background process management (fork, PID files, stop/status) |
| `internal/funnel` | Tailscale Funnel integration for public internet access |

---

## Endpoints

### `GET /healthz`
Returns `200 OK` with body `ok\n`. No authentication required.

### `POST /webhook`
Main webhook receiver. Requires valid `X-Hub-Signature-256` header.

| Status | Meaning |
|---|---|
| 202 Accepted | Workflow matched and dispatched |
| 200 OK | Event received but no workflow matched (or action filtered out) |
| 403 Forbidden | Invalid or missing webhook signature |
| 400 Bad Request | Invalid JSON payload |
| 413 Too Large | Payload exceeds 10 MB |
| 405 Not Allowed | Non-POST request |

---

## Supported GitHub Events

| Event | Action Filter | Match String |
|---|---|---|
| `issue_comment` | `created` only | Comment body |
| `pull_request_review_comment` | `created` only | Comment body |
| `pull_request_review` | `submitted` only | Review body |
| `pull_request` | None (all actions) | `<action>:<merge_status>` (e.g. `opened:unmerged`, `closed:merged`) |

Default events (when not specified per-workflow): `issue_comment`, `pull_request_review_comment`, `pull_request_review`.

---

## Configuration

**Location:** `~/.hookrunner/config.yaml` (override with `--config`)

```yaml
webhook_secret: "your-secret-here"     # Required. HMAC-SHA256 secret.
port: 8443                             # Optional. Default: 8443. Must be 443, 8443, or 10000 when Funnel is enabled.

funnel:
  enabled: true                        # Optional. Enable Tailscale Funnel.
  url: ""                              # Optional. Custom Funnel URL.

daemon:
  pid_file: "~/.hookrunner/hookrunner.pid"
  log_file: "~/.hookrunner/hookrunner.log"

workflows:
  claude-review:
    trigger: '/cc'                      # Required. Regex to match against event body.
    command: 'claude -p "Review PR #{{.PRNumber}} in {{.RepoFullName}}"'
    workdir: '/path/to/repos/{{.RepoFullName}}'  # Optional. Supports templates.
    timeout: 300                       # Optional. Seconds. Default: 300.
    events:                            # Optional. Defaults to comment/review events.
      - issue_comment
      - pull_request_review_comment
      - pull_request_review
    authors:                           # Optional. Empty = all authors allowed.
      - octocat
```

### Template Variables

Available in `command` and `workdir` fields using Go template syntax:

| Variable | Description |
|---|---|
| `{{.RepoFullName}}` | `org/repo` |
| `{{.RepoCloneURL}}` | Git clone URL |
| `{{.PRNumber}}` | PR or issue number |
| `{{.CommentBody}}` | Comment or review text |
| `{{.CommentAuthor}}` | GitHub username |
| `{{.EventType}}` | Event type string |

### Environment Variables Passed to Workflows

All workflows receive these env vars:

| Variable | Description |
|---|---|
| `HR_PR_NUMBER` | PR/issue number |
| `HR_REPO` | Repository full name |
| `HR_COMMENT_BODY` | Comment or review text |
| `HR_COMMENT_AUTHOR` | GitHub username |
| `HR_EVENT_TYPE` | Event type string |

---

## CLI Flags

| Flag | Description |
|---|---|
| `--config <path>` | Config file path (default: `~/.hookrunner/config.yaml`) |
| `--daemon` | Run as background daemon |
| `--stop` | Stop running daemon |
| `--status` | Check daemon status |
| `--port <n>` | Override config port |
| `--no-funnel` | Disable Tailscale Funnel |
| `--init` | Generate default config file |
| `--version` | Print version |

---

## Security

- **Signature verification:** All webhooks validated via HMAC-SHA256 with constant-time comparison. Requires `X-Hub-Signature-256` header.
- **Input sanitization:** All template variables are stripped of shell metacharacters (`;`, `&`, `|`, `$`, backticks, etc.) before being interpolated into commands.
- **Localhost binding:** Server binds to `127.0.0.1` only. Internet exposure requires Tailscale Funnel.
- **File permissions:** Config, PID, and log files created with `0600`; directories with `0700`.
- **Author filtering:** Optional per-workflow allowlist of GitHub usernames (case-insensitive).

---

## Filtering Pipeline

Workflows are evaluated in this order. The first matching workflow is dispatched:

1. **Event type** -- Is the incoming event in the workflow's `events` list?
2. **Action** -- For comments, is the action `created`? For reviews, is it `submitted`?
3. **Author** -- If the workflow has an `authors` list, is the commenter on it?
4. **Trigger regex** -- Does the comment/review body (or PR status string) match the `trigger` pattern?

---

## Workflow Execution

- Commands run via `sh -c <rendered_command>`.
- Execution is **asynchronous** (dispatched in a goroutine; HTTP returns 202 immediately).
- Combined stdout/stderr is captured and logged.
- Timeout enforced via `context.WithTimeout`; process killed on expiry.
- Non-zero exit codes are logged as errors.

---

## Daemon Mode

```bash
hookrunner --daemon           # Start as background process
hookrunner --status           # Check if running
hookrunner --stop             # Stop (SIGTERM, then SIGKILL after 5s)
```

- Uses `Setsid` to create a new process session.
- PID written to configured `pid_file`.
- Logs written to configured `log_file`.
- Graceful shutdown on SIGINT/SIGTERM with 5-second HTTP drain timeout.

---

## Tailscale Funnel

When `funnel.enabled: true`, hookrunner runs `tailscale funnel --https <port> http://localhost:<port>` to expose the local server to the internet via Tailscale's infrastructure. This is how GitHub can reach a local machine without port forwarding.

**Port restrictions:** Tailscale Funnel only supports HTTPS on ports **443**, **8443**, or **10000**. Config validation enforces this when Funnel is enabled.

**Prerequisites:**
1. Tailscale installed and logged in.
2. HTTPS certificates enabled in the [Tailscale admin DNS settings](https://login.tailscale.com/admin/dns).
3. Funnel enabled in [Tailscale ACL policy](https://login.tailscale.com/admin/acls/file) via a `nodeAttrs` block:
   ```json
   { "nodeAttrs": [{ "target": ["autogroup:member"], "attr": ["funnel"] }] }
   ```
4. If using the macOS App Store version, the CLI is at `/Applications/Tailscale.app/Contents/MacOS/Tailscale`. Add an alias to your shell profile:
   ```bash
   alias tailscale="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
   ```

**Other details:**
- Disabled with `--no-funnel` flag.
- Funnel process is cleaned up on shutdown.
- If `tailscale` CLI is not available, a warning is logged but the server still starts.

---

## Dependencies

- **Go 1.25.6**
- **gopkg.in/yaml.v3** -- YAML config parsing
- **tailscale CLI** -- Optional, for Funnel feature
- Standard library only otherwise (crypto, net/http, os/exec, text/template, etc.)

---

## Building and Running

```bash
# Build
go build -o hookrunner ./cmd/hookrunner

# Initialize config
./hookrunner --init

# Edit config
$EDITOR ~/.hookrunner/config.yaml

# Run in foreground
./hookrunner

# Run as daemon
./hookrunner --daemon
```

---

## Typical Setup Flow

1. Run `hookrunner --init` to generate `~/.hookrunner/config.yaml`.
2. Set `webhook_secret` to a strong random value.
3. Configure one or more workflows with trigger patterns and commands.
4. Start hookrunner (with `--daemon` for background operation).
5. In your GitHub repo settings, add a webhook:
   - **Payload URL:** Your Tailscale Funnel URL + `/webhook` (e.g. `https://<your-machine>.<tailnet>.ts.net:8443/webhook`)
   - **Content type:** `application/json`
   - **Secret:** Same value as `webhook_secret`
   - **Events:** Select the events your workflows need (issues, pull requests, PR reviews)
6. Comment `/cc` (or whatever your trigger pattern is) on a PR to test.
