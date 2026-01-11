#!/bin/bash
set -e

# Tailwind CSS build script
# Downloads Tailwind CLI if not present and builds CSS

TAILWIND_VERSION="v3.4.1"
TAILWIND_BIN="./bin/tailwindcss"
PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

cd "$PROJECT_ROOT"

# Detect platform
detect_platform() {
    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)

    case "$os" in
        darwin)
            case "$arch" in
                arm64) echo "macos-arm64" ;;
                x86_64) echo "macos-x64" ;;
                *) echo "macos-arm64" ;;
            esac
            ;;
        linux)
            case "$arch" in
                aarch64|arm64) echo "linux-arm64" ;;
                x86_64) echo "linux-x64" ;;
                armv7l) echo "linux-armv7" ;;
                *) echo "linux-x64" ;;
            esac
            ;;
        *)
            echo "linux-x64"
            ;;
    esac
}

# Download Tailwind CLI if not exists
if [ ! -f "$TAILWIND_BIN" ]; then
    echo "Downloading Tailwind CSS CLI..."
    mkdir -p ./bin
    PLATFORM=$(detect_platform)
    DOWNLOAD_URL="https://github.com/tailwindlabs/tailwindcss/releases/download/${TAILWIND_VERSION}/tailwindcss-${PLATFORM}"

    echo "Platform: $PLATFORM"
    echo "URL: $DOWNLOAD_URL"

    curl -sL "$DOWNLOAD_URL" -o "$TAILWIND_BIN"
    chmod +x "$TAILWIND_BIN"
    echo "Tailwind CLI downloaded successfully"
fi

# Build CSS
echo "Building CSS..."
$TAILWIND_BIN \
    -c internal/web/tailwind/tailwind.config.js \
    -i internal/web/tailwind/input.css \
    -o internal/web/static/style.css \
    --minify

echo "CSS build complete: internal/web/static/style.css"
