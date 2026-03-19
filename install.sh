#!/bin/sh
set -e

# Knowledge Broker installer
# Usage: curl -fsSL https://knowledgebroker.dev/install.sh | sh
#
# Installs the latest kb binary for your platform.
# Supports macOS (arm64, amd64) and Linux (amd64).

REPO="alecgard/knowledge-broker"
INSTALL_DIR="/usr/local/bin"
FALLBACK_DIR="${HOME}/.local/bin"

main() {
    os="$(detect_os)"
    arch="$(detect_arch)"

    printf "Detected platform: %s/%s\n" "$os" "$arch"

    version="$(latest_version)"
    if [ -z "$version" ]; then
        err "could not determine latest release version"
    fi
    printf "Latest version: %s\n" "$version"

    tarball="kb-${version}-${os}-${arch}.tar.gz"
    url="https://github.com/${REPO}/releases/download/${version}/${tarball}"

    tmpdir="$(mktemp -d)"
    trap 'rm -rf "$tmpdir"' EXIT

    printf "Downloading %s...\n" "$tarball"
    if ! curl -fsSL -o "${tmpdir}/${tarball}" "$url"; then
        err "download failed — check that a release exists for ${os}/${arch} at:\n  https://github.com/${REPO}/releases/tag/${version}"
    fi

    tar xzf "${tmpdir}/${tarball}" -C "$tmpdir"

    if [ ! -f "${tmpdir}/kb" ]; then
        err "archive did not contain a 'kb' binary"
    fi

    install_binary "${tmpdir}/kb"

    printf "\nInstalled kb %s\n" "$version"
    printf "Run 'kb --help' to get started.\n"
}

detect_os() {
    case "$(uname -s)" in
        Darwin) echo "darwin" ;;
        Linux)  echo "linux" ;;
        *)      err "unsupported OS: $(uname -s) — only macOS and Linux are supported" ;;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             err "unsupported architecture: $(uname -m)" ;;
    esac
}

latest_version() {
    # Use GitHub API to get latest release tag.
    curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null \
        | grep '"tag_name"' \
        | head -1 \
        | sed 's/.*"tag_name": *"//;s/".*//'
}

install_binary() {
    src="$1"

    # Try /usr/local/bin first.
    if [ -w "$INSTALL_DIR" ]; then
        mv "$src" "${INSTALL_DIR}/kb"
        chmod +x "${INSTALL_DIR}/kb"
        printf "Installed to %s/kb\n" "$INSTALL_DIR"
        return
    fi

    # Try with sudo.
    if command -v sudo >/dev/null 2>&1; then
        printf "Installing to %s (requires sudo)...\n" "$INSTALL_DIR"
        if sudo mv "$src" "${INSTALL_DIR}/kb" && sudo chmod +x "${INSTALL_DIR}/kb"; then
            printf "Installed to %s/kb\n" "$INSTALL_DIR"
            return
        fi
    fi

    # Fall back to ~/.local/bin.
    mkdir -p "$FALLBACK_DIR"
    mv "$src" "${FALLBACK_DIR}/kb"
    chmod +x "${FALLBACK_DIR}/kb"
    printf "Installed to %s/kb\n" "$FALLBACK_DIR"

    # Check if fallback dir is on PATH.
    case ":${PATH}:" in
        *":${FALLBACK_DIR}:"*) ;;
        *)
            printf "\n%s is not in your PATH.\n" "$FALLBACK_DIR"
            printf "Add it by running:\n"
            printf "  export PATH=\"%s:\$PATH\"\n" "$FALLBACK_DIR"
            ;;
    esac
}

err() {
    printf "Error: %b\n" "$1" >&2
    exit 1
}

main
