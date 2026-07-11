#!/bin/bash
# jj-stacked installer
# https://github.com/OSMorph/jj-stacked
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/OSMorph/jj-stacked/main/install.sh | bash
#   curl -sSL ... | bash -s -- --version v0.1.0
#   curl -sSL ... | bash -s -- --prefix /usr/local

set -euo pipefail

REPO="OSMorph/jj-stacked"
INSTALL_DIR="${HOME}/.local/bin"
VERSION=""

# Colors (disabled if not a terminal)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

info() {
    echo -e "${BLUE}==>${NC} $1"
}

success() {
    echo -e "${GREEN}==>${NC} $1"
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $1"
}

error() {
    echo -e "${RED}Error:${NC} $1" >&2
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --version)
            [[ $# -ge 2 && -n "${2:-}" ]] || error "--version requires a value"
            VERSION="$2"
            shift 2
            ;;
        --prefix)
            [[ $# -ge 2 && -n "${2:-}" ]] || error "--prefix requires a value"
            INSTALL_DIR="$2/bin"
            shift 2
            ;;
        --help|-h)
            echo "jj-stacked installer"
            echo ""
            echo "Usage:"
            echo "  curl -sSL https://raw.githubusercontent.com/OSMorph/jj-stacked/main/install.sh | bash"
            echo ""
            echo "Options:"
            echo "  --version VERSION  Install a specific version (e.g., v0.1.0)"
            echo "  --prefix PATH      Install to PATH/bin (default: ~/.local)"
            echo "  --help             Show this help message"
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            ;;
    esac
done

# Detect OS
detect_os() {
    local os
    os="$(uname -s)"
    case "$os" in
        Linux*)  echo "linux" ;;
        Darwin*) echo "darwin" ;;
        MINGW*|MSYS*|CYGWIN*|Windows_NT)
            error "Windows is not supported by this installer.
Please download manually from: https://github.com/${REPO}/releases
Or use: go install github.com/${REPO}/cmd/jj-stacked@latest"
            ;;
        *)
            error "Unsupported operating system: $os"
            ;;
    esac
}

# Detect architecture
detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)
            error "Unsupported architecture: $arch"
            ;;
    esac
}

# Get latest version from GitHub API
get_latest_version() {
    local url="https://api.github.com/repos/${REPO}/releases/latest"
    local version

    if command -v curl &> /dev/null; then
        version=$(curl -sS "$url" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    elif command -v wget &> /dev/null; then
        version=$(wget -qO- "$url" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    else
        error "Neither curl nor wget found. Please install one of them."
    fi

    if [ -z "$version" ]; then
        error "Failed to fetch latest version. Please specify a version with --version"
    fi

    echo "$version"
}

# Download file
download() {
    local url="$1"
    local output="$2"

    if command -v curl &> /dev/null; then
        curl -fsSL "$url" -o "$output"
    elif command -v wget &> /dev/null; then
        wget -q "$url" -O "$output"
    else
        error "Neither curl nor wget found. Please install one of them."
    fi
}

# Verify checksum
verify_checksum() {
    local file="$1"
    local checksums_file="$2"
    local filename
    filename=$(basename "$file")

    local expected
    expected=$(awk -v filename="$filename" '{ candidate=$NF; sub(/^\*/, "", candidate); if (candidate == filename) { print $1; exit } }' "$checksums_file")

    if [ -z "$expected" ]; then
        error "Checksum not found for $filename"
    fi

    local actual
    if command -v sha256sum &> /dev/null; then
        actual=$(sha256sum "$file" | awk '{print $1}')
    elif command -v shasum &> /dev/null; then
        actual=$(shasum -a 256 "$file" | awk '{print $1}')
    else
        error "Neither sha256sum nor shasum found; cannot verify the download"
    fi

    if [ "$expected" != "$actual" ]; then
        error "Checksum verification failed!
Expected: $expected
Actual:   $actual"
    fi

    info "Checksum verified"
}

main() {
    echo ""
    echo "  jj-stacked installer"
    echo "  https://github.com/${REPO}"
    echo ""

    # Detect platform
    local os arch
    os=$(detect_os)
    arch=$(detect_arch)
    info "Detected platform: ${os}/${arch}"

    # Get version
    if [ -z "$VERSION" ]; then
        info "Fetching latest version..."
        VERSION=$(get_latest_version)
    fi
    info "Installing version: ${VERSION}"

    # Version without 'v' prefix for archive name
    local version_num="${VERSION#v}"

    # Construct download URLs
    local archive_name="jj-stacked_${version_num}_${os}_${arch}.tar.gz"
    local base_url="https://github.com/${REPO}/releases/download/${VERSION}"
    local archive_url="${base_url}/${archive_name}"
    local checksums_url="${base_url}/checksums.txt"

    # Create temp directory
    local tmpdir
    tmpdir=$(mktemp -d)
    trap "rm -rf '$tmpdir'" EXIT

    # Download archive and checksums
    info "Downloading ${archive_name}..."
    download "$archive_url" "${tmpdir}/${archive_name}"

    info "Downloading checksums..."
    download "$checksums_url" "${tmpdir}/checksums.txt"

    # Verify checksum
    verify_checksum "${tmpdir}/${archive_name}" "${tmpdir}/checksums.txt"

    # Extract archive
    info "Extracting..."
    tar -xzf "${tmpdir}/${archive_name}" -C "$tmpdir"

    # Create install directory if needed
    mkdir -p "$INSTALL_DIR"

    # Install binaries
    info "Installing to ${INSTALL_DIR}..."
    install -m 0755 "${tmpdir}/jj-stacked" "$INSTALL_DIR/jj-stacked"
    install -m 0755 "${tmpdir}/jjk" "$INSTALL_DIR/jjk"
    chmod +x "${INSTALL_DIR}/jj-stacked" "${INSTALL_DIR}/jjk"

    echo ""
    success "jj-stacked ${VERSION} installed successfully!"
    echo ""

    # Check if install directory is in PATH
    if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
        warn "${INSTALL_DIR} is not in your PATH"
        echo ""
        echo "Add it to your PATH by adding this to your shell profile:"
        echo ""
        echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
        echo ""
    else
        echo "Run 'jjk --version' to verify the installation."
        echo ""
    fi
}

main "$@"
