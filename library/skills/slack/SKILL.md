---
name: slack
description: Slack CLI wrapper for posting messages, reading channels, threading replies, and searching. Use this skill whenever the user mentions Slack, a channel, DM, a message thread, or wants to notify someone. Also trigger when an agent needs to post a status update, surface a finding, or relay output to a Slack channel. Invoked with /slack.
user-invocable: true
---

# Slack CLI

Interface with Slack for messaging, channel reads, and search using a thin
curl/jq wrapper installed with your toolkit.

## Discovery

The wrapper script is `slack-api`. Check for it in this order:

```bash
which slack-api                          # if in PATH
~/.forge/skills/slack/bin/slack-api      # installed via forge sync
```

If not found, install it:

```bash
# Copy the wrapper into PATH
cp -f ~/.forge/skills/slack/bin/slack-api ~/.local/bin/slack-api
chmod +x ~/.local/bin/slack-api
```

Dependencies: `curl` and `jq` (both standard on macOS; `brew install jq` if missing).

Run `slack-api help` to see the full command surface — check it after a toolkit
update in case new subcommands were added.

## Auth

Slack auth is a bot token stored in an environment variable — **never hardcoded**:

```bash
export SLACK_TOKEN=xoxb-...    # bot token from your Slack app
```

To make it persistent, add to `~/.zshrc` or `~/.bashrc`. The token never
touches the repo.

To verify auth:

```bash
slack-api whoami
```

To get a bot token: create a Slack app at https://api.slack.com/apps, grant
it the scopes below under "OAuth & Permissions", and install to your workspace.

**Required bot token scopes:**
- `chat:write` — post messages
- `channels:read`, `groups:read`, `im:read`, `mpim:read` — list channels
- `channels:history`, `groups:history`, `im:history`, `mpim:history` — read messages
- `search:read` — search messages (requires user token for full-workspace search)
- `users:read` — list members
- `files:write` — upload files

## Common Operations

**Check who you're authenticated as:**
```bash
slack-api whoami
```

**List channels:**
```bash
slack-api channels
# Returns: id, name, is_private, num_members
```

**Post a message:**
```bash
slack-api post "#general" "Deployment complete."
slack-api post C0123ABC456 "Deployment complete."   # use channel ID when you have it
```

**Reply in a thread:**
```bash
slack-api post "#general" "Follow-up." --thread 1234567890.123456
```

**Read recent messages:**
```bash
slack-api history C0123ABC456          # last 20 messages
slack-api history C0123ABC456 50       # last 50 messages
```

**Read a thread:**
```bash
slack-api thread C0123ABC456 1234567890.123456
```

**Send a DM:**
```bash
slack-api dm U0123ABC456 "Hey, deploy is done."
```

**Search messages:**
```bash
slack-api search "deployment failed" 20
```

**Upload a file:**
```bash
slack-api upload C0123ABC456 report.txt "Here's the summary"
```

**List workspace members:**
```bash
slack-api users
# Returns: id, name, real_name
```

## Gotchas

- **Channel arg accepts IDs or names**: `C0123...` IDs are more stable than
  names (channels can be renamed). Prefer IDs when you have them from a prior
  `channels` call.
- **`search` requires a user token** for full-workspace search (`xoxp-...`).
  A bot token can only search channels the bot has been added to. Add the bot
  to relevant channels before searching.
- **DMs use user IDs** (`U0123...`), not usernames. Get the ID from
  `slack-api users` or `slack-api whoami`.
- **Thread timestamps** are the `ts` field on the parent message, not a
  human-readable timestamp. Capture it from `history` output before replying.
- **Output is JSON** — pipe to `jq` for further processing, or use the
  pre-filtered output (each subcommand returns the most useful fields).
- **Rate limits**: Slack Tier 3 = 50 req/min. Add `sleep 1.5` between bulk calls.

## Conventions

- **Prefer channel IDs over names** in scripts — names change, IDs don't.
- **Use threads** for follow-up messages to keep channels clean.
- **Status updates to a designated channel** (e.g. `#deploys`) are the standard
  pattern for agent notifications — avoid DM spam.
- **Never hardcode the token** — always read from `$SLACK_TOKEN`.
