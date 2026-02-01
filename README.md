# hookrunner

A lightweight local server that listens for GitHub webhook events, watches for `/cc` mentions in PRs and issues, and dispatches local workflows in response.

## Quick Start

```bash
# Build
go build -o hookrunner ./cmd/hookrunner

# Generate default config
./hookrunner --init

# Edit config with your webhook secret and workflows
$EDITOR ~/.hookrunner/config.yaml

# Run
./hookrunner
```

## GitHub Webhook Setup

1. Generate a secret: `openssl rand -hex 32`
2. Set it as `webhook_secret` in `~/.hookrunner/config.yaml`
3. In your repo, go to **Settings > Webhooks > Add webhook**:
   - **Payload URL:** `https://<your-machine>.<tailnet>.ts.net:8443/webhook`
   - **Content type:** `application/json`
   - **Secret:** Same value as `webhook_secret`
   - **Events:** Issue comments, Pull request reviews, Pull request review comments

## Tailscale Funnel

hookrunner uses [Tailscale Funnel](https://tailscale.com/kb/1311/tailscale-funnel) to expose the local server to the internet. Ensure:

1. Tailscale is installed and logged in
2. HTTPS certificates are enabled in [admin DNS settings](https://login.tailscale.com/admin/dns)
3. Funnel is enabled in your [ACL policy](https://login.tailscale.com/admin/acls/file)

**macOS App Store users:** The CLI is bundled at `/Applications/Tailscale.app/Contents/MacOS/Tailscale`. Add to your shell profile:

```bash
alias tailscale="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
```

Port must be 443, 8443, or 10000 when Funnel is enabled (default: 8443).

If hookrunner's built-in Funnel integration doesn't start correctly, you can run it manually:

```bash
tailscale funnel --bg --https=<port> http://localhost:<port>
```

Verify with:

```bash
tailscale funnel status
```

## Configuration

See [`HANDOFF.md`](HANDOFF.md) for full configuration reference, template variables, environment variables, filtering pipeline, and architecture details.

## Daemon Mode

```bash
./hookrunner --daemon    # Start in background
./hookrunner --status    # Check if running
./hookrunner --stop      # Stop daemon
```
