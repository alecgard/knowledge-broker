#!/usr/bin/env bash
# Ingest ACME test data from Confluence and Slack.
# Requires .env to be configured with KB_CONFLUENCE_* and KB_SLACK_* credentials.
# Usage: ./testdata/ingest-acme.sh [--db path/to/db]

set -euo pipefail
cd "$(dirname "$0")/.."

DB="${1:-testdata/acme-test.db}"
if [[ "${1:-}" == "--db" ]]; then
  DB="${2:-testdata/acme-test.db}"
fi

echo "=== ACME Test Data Ingestion ==="
echo "DB: $DB"
echo

# Confluence: ACME space (orchatest.atlassian.net)
echo "--- Confluence (ACME space) ---"
go run ./cmd/kb ingest --confluence ACME --db "$DB"
echo

# Slack: all ACME channels
echo "--- Slack (acme-haf5895) ---"
go run ./cmd/kb ingest \
  --slack C0AKB4GRELF \
  --slack C0AKL6BKDGB \
  --slack C0AKB4GPPST \
  --slack C0AKSHSK8MQ \
  --slack C0AKL6BE7NX \
  --slack C0AKQG1KDKQ \
  --slack C0AKL0BGGV9 \
  --slack C0AKSBT0ZQS \
  --db "$DB"
echo

echo "=== Done ==="
echo "Query with: go run ./cmd/kb query --db $DB --human \"your question\""
