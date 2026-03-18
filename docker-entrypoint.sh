#!/bin/sh
set -e

# Load environment from .env if present
if [ -f /data/.env ]; then
    set -a
    . /data/.env
    set +a
fi

# Start Ollama in the background
ollama serve &
OLLAMA_PID=$!

# Wait for Ollama to be ready
for i in $(seq 1 30); do
    if curl -sf http://localhost:11434/api/tags >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

# Pull the default embedding model if not already present
ollama pull nomic-embed-text 2>/dev/null || true

# Run kb with all arguments
exec kb "$@"
