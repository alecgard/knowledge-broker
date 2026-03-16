#!/usr/bin/env bash
# Generate llms-full.txt from all doc pages, stripped of YAML frontmatter.
set -euo pipefail

DOCS_DIR="$(cd "$(dirname "$0")/../docs" && pwd)"
OUT="$DOCS_DIR/llms-full.txt"

# Order matters — most important pages first
pages=(
  index.md
  quickstart.md
  deployment.md
  architecture.md
  connectors.md
  mcp.md
  cli.md
  eval.md
)

{
  echo "# Knowledge Broker — Complete Documentation"
  echo ""
  echo "Source: https://knowledgebroker.dev"
  echo "Generated: $(date -u +%Y-%m-%d)"
  echo ""

  for page in "${pages[@]}"; do
    file="$DOCS_DIR/$page"
    if [ -f "$file" ]; then
      # Strip YAML frontmatter (lines between --- delimiters)
      awk 'BEGIN{skip=0} /^---$/{skip++; next} skip<2{next} {print}' "$file"
      echo ""
      echo "---"
      echo ""
    fi
  done
} > "$OUT"

echo "Generated $OUT ($(wc -l < "$OUT") lines)"
