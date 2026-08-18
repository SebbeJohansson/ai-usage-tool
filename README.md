# ai-usage-mini

Watches two numbers for one person:

1. **Claude.ai quota meters** — the "session" and "weekly" usage bars the
   Claude web app shows, for whichever account owns the configured session
   cookie.
2. **GitHub Copilot AI-credit usage** — this month's included/overage
   credits for one login, on github.com.

It polls on a timer (default every 15 minutes) and posts to Discord only
when a Claude meter climbs into a step it hasn't already been announced at —
25-point steps for `session` (it resets roughly every 5 hours, so anything
finer would fire constantly), 5-point steps for everything else. Copilot
credit usage has no step of its own; it rides along as context in whatever
message the Claude side sends, since one person's draw against an org-wide
credit pool isn't a meaningful percentage by itself.

## Run it

```zsh
cp .env.example .env
$EDITOR .env
go build ./cmd/aiusage

./aiusage --once   # single poll, good for checking credentials
./aiusage          # loops forever
```

## Run it in Docker

```zsh
mkdir -p data && chown 65532:65532 data   # image runs as the distroless nonroot uid
docker compose up -d --build
docker compose logs -f
```

`./data` holds the alert ledger (which steps have already been announced),
so a container restart doesn't re-announce or silently drop a rollover.
Nothing else is persisted — there's no history, just the current reading and
what's already been said about it.

## Getting credentials

| Variable | How to get it |
|---|---|
| `CLAUDE_ORG_ID` | the org id segment in any `claude.ai/settings/...` URL |
| `CLAUDE_SESSION_KEY` | the `sessionKey` cookie value from a logged-in claude.ai tab (browser DevTools → Application → Cookies) — it rotates periodically, so re-copy it when polls start failing |
| `GITHUB_TOKEN` | a token with org billing/admin read access — classic PAT with `manage_billing:copilot` + `read:org`, or fine-grained with org "Administration" (read) |
| `GITHUB_ORG` / `GITHUB_USER` | the org granting the Copilot seat, and the login to watch |
| `DISCORD_WEBHOOK_URL` | Discord → Server Settings → Integrations → Webhooks. Webhooks are bound to whatever channel they're created in — Discord has no such thing as a "DM webhook" — so for private-to-you delivery, create the webhook on a channel only you can see (e.g. a personal server with nobody else in it), not a shared one. |

## Layout

```
cmd/aiusage/              polling loop, alert-step logic, message composition
internal/claudeusage/     Claude.ai quota-meter client
internal/copilotcredit/   GitHub Copilot AI-credit usage client
internal/notify/          Discord webhook sender
internal/thresholds/      on-disk ledger of already-announced steps
internal/settings/        environment variable loading
```
