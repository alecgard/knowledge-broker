---
description: Connect Knowledge Broker to Git repos, Confluence, Slack, and GitHub Wiki. Setup instructions and authentication for each source type.
---

# Connectors

KB uses pluggable connectors to ingest content from different sources. Each connector scans a source, produces documents, and supports incremental re-ingestion via checksums.

## Local Filesystem

Ingest files from a local directory. Walks the directory tree recursively, respects `.gitignore` patterns.

```bash
kb ingest --source ./path/to/dir
kb ingest --source ./repo-a --source ./repo-b   # multiple directories
```

No configuration needed. This is the default if no flags are given — `kb ingest` ingests the current directory.

## Git

Clone and ingest a Git repository by URL. Supports public repos directly; private repos authenticate via `KB_GITHUB_TOKEN`, the `gh` CLI, or GitHub device flow. GitLab and other Git hosts are also supported.

```bash
kb ingest --git https://github.com/owner/repo
kb ingest --git https://github.com/owner/private-repo   # uses gh CLI or device flow
kb ingest --git https://gitlab.com/owner/repo            # uses KB_GITLAB_TOKEN
```

| Variable | Required | Description |
|----------|----------|-------------|
| `KB_GITHUB_TOKEN` | No | GitHub personal access token for private repos |
| `KB_GITLAB_TOKEN` | No | GitLab personal access token for private repos |
| `KB_GIT_TOKEN` | No | Generic Git token (works with any host) |
| `KB_GITHUB_CLIENT_ID` | No | GitHub OAuth app client ID for device flow auth |

For GitHub, if no token is set, KB tries the `gh` CLI's cached token, then falls back to device flow if a client ID is configured.

## Confluence

Ingest pages from an Atlassian Confluence Cloud space. Fetches all pages via the REST API with pagination, extracts body content, and supports incremental sync.

```bash
kb ingest --confluence ENGINEERING
kb ingest --confluence ENGINEERING --confluence PRODUCT   # multiple spaces
```

| Variable | Required | Description |
|----------|----------|-------------|
| `KB_CONFLUENCE_BASE_URL` | Yes | Your Confluence instance URL (e.g., `https://yoursite.atlassian.net`) |
| `KB_CONFLUENCE_EMAIL` | Yes | Email address for API authentication |
| `KB_CONFLUENCE_TOKEN` | Yes | Atlassian API token ([create one here](https://id.atlassian.com/manage-profile/security/api-tokens)) |

The `--confluence` flag takes a **space key** (the short code visible in Confluence URLs, e.g., `ENGINEERING`). Each space is ingested as a separate source.

Pages that are deleted in Confluence are automatically detected and removed from the knowledge base on re-ingestion.

## Slack

Ingest messages from Slack channels. Fetches message history via the Slack Web API. Threaded conversations become individual documents; non-threaded messages are grouped by day.

```bash
kb ingest --slack C0ABC123DEF
kb ingest --slack C0ABC123DEF --slack C0XYZ789GHI   # multiple channels
```

| Variable | Required | Description |
|----------|----------|-------------|
| `KB_SLACK_TOKEN` | Yes | Bot User OAuth Token (`xoxb-...`) |
| `KB_SLACK_WORKSPACE` | No | Workspace name for display (e.g., `acme-org`) |

### Slack app setup

1. Create a Slack app at [api.slack.com/apps](https://api.slack.com/apps)
2. Add the following **Bot Token Scopes** under OAuth & Permissions:
   - `channels:history` — read message history
   - `channels:read` — list channels and get channel info
   - `channels:join` — join public channels (if the bot isn't already a member)
3. Install the app to your workspace
4. Copy the **Bot User OAuth Token** (`xoxb-...`) to `KB_SLACK_TOKEN`
5. Find channel IDs: right-click a channel name in Slack → "View channel details" → the ID is at the bottom

By default, Slack ingestion looks back **90 days**. Messages older than that are not fetched.

### How messages are structured

- **Threaded conversations** (messages with replies): each thread becomes a single document containing the parent message and all replies
- **Non-threaded messages**: grouped into daily documents per channel

## GitHub Wiki

Ingest pages from a GitHub repository's wiki. The wiki is a separate Git repo (`{repoURL}.wiki.git`) that KB clones and scans. Page links are rewritten to point to the GitHub wiki web UI.

```bash
kb ingest --wiki https://github.com/owner/repo
kb ingest --wiki https://github.com/owner/repo --wiki https://github.com/owner/other-repo
```

| Variable | Required | Description |
|----------|----------|-------------|
| `KB_GITHUB_TOKEN` | No | Required for private repos |

The `--wiki` flag takes the **main repository URL** (not the wiki URL). KB automatically derives the wiki clone URL.

Authentication works the same as the Git connector — `KB_GITHUB_TOKEN`, `gh` CLI, or device flow.

## Combining sources

All connector flags can be mixed in a single ingest command:

```bash
kb ingest \
  --source ./local-docs \
  --git https://github.com/acme/backend \
  --confluence ENGINEERING \
  --slack C0ABC123DEF \
  --wiki https://github.com/acme/platform
```

Each source is tracked independently. Re-running the same command only processes new or changed content.

## Incremental ingestion

All connectors support incremental ingestion via SHA-256 checksums. On each run:

1. KB loads known checksums from the database
2. The connector scans the source and compares against known checksums
3. Only new or changed documents are extracted, embedded, and stored
4. Documents that existed previously but are no longer present are deleted from the database

To force a full re-ingestion, remove the source first:

```bash
kb sources remove confluence/ENGINEERING
kb ingest --confluence ENGINEERING
```

## Re-ingesting all sources

To re-ingest all previously registered local sources:

```bash
kb ingest --all
```

This is useful after upgrading KB or changing chunking/embedding settings.
