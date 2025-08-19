#!/bin/bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
INSTALL_DIR="$HOME/.miren"
BINARY_NAME="miren"

# Functions
print_error() {
    echo -e "${RED}Error: $1${NC}" >&2
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_info() {
    echo -e "${YELLOW}→ $1${NC}"
}

detect_os() {
    if [[ "$OSTYPE" == "darwin"* ]]; then
        echo "macos"
    elif [[ "$OSTYPE" == "linux"* ]]; then
        echo "linux"
    else
        echo "unsupported"
    fi
}

detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *) echo "unsupported" ;;
    esac
}

check_docker() {
    if ! command -v docker &> /dev/null; then
        return 1
    fi
    
    # Check if Docker daemon is running
    if ! docker info &> /dev/null; then
        return 1
    fi
    
    return 0
}

verify_checksum() {
    local file_path="$1"
    local checksum_url="$2"
    local temp_checksum="/tmp/$(basename "$file_path").sha256"
    
    # Try to download the checksum file
    if curl -fsSL --connect-timeout 5 --retry 2 "$checksum_url" -o "$temp_checksum" 2>/dev/null; then
        print_info "Verifying checksum..."
        
        # Check if sha256sum is available
        if command -v sha256sum &>/dev/null; then
            local expected_checksum=$(cat "$temp_checksum" | cut -d' ' -f1)
            local actual_checksum=$(sha256sum "$file_path" | cut -d' ' -f1)
            
            if [ "$expected_checksum" = "$actual_checksum" ]; then
                print_success "Checksum verification passed"
                rm -f "$temp_checksum"
                return 0
            else
                print_error "Checksum verification failed!"
                print_error "Expected: $expected_checksum"
                print_error "Actual: $actual_checksum"
                rm -f "$temp_checksum" "$file_path"
                exit 1
            fi
        elif command -v shasum &>/dev/null; then
            # macOS fallback
            local expected_checksum=$(cat "$temp_checksum" | cut -d' ' -f1)
            local actual_checksum=$(shasum -a 256 "$file_path" | cut -d' ' -f1)
            
            if [ "$expected_checksum" = "$actual_checksum" ]; then
                print_success "Checksum verification passed"
                rm -f "$temp_checksum"
                return 0
            else
                print_error "Checksum verification failed!"
                print_error "Expected: $expected_checksum"
                print_error "Actual: $actual_checksum"
                rm -f "$temp_checksum" "$file_path"
                exit 1
            fi
        else
            print_info "No checksum utility available, skipping verification"
            rm -f "$temp_checksum"
            return 0
        fi
    else
        print_info "Checksum not available, skipping verification"
        return 0
    fi
}

install_linux() {
    print_info "Installing Miren for Linux..."
    
    # Download release package
    local arch=$(detect_arch)
    local version="${MIREN_VERSION:-main}"
    local download_url="https://api.miren.cloud/assets/release/miren/${version}/miren-base-linux-${arch}.tar.gz"
    
    print_info "Downloading miren package..."
    curl -fSL --connect-timeout 10 --retry 3 "$download_url" -o /tmp/miren.tar.gz || {
        print_error "Failed to download miren package"
        exit 1
    }
    
    # Verify checksum if available
    local checksum_url="https://api.miren.cloud/assets/release/miren/${version}/miren-base-linux-${arch}.tar.gz.sha256"
    verify_checksum "/tmp/miren.tar.gz" "$checksum_url"
    
    # Create installation directory
    mkdir -p "$INSTALL_DIR/bin"
    
    # Extract package
    print_info "Extracting miren components..."
    tar -xzf /tmp/miren.tar.gz -C "$INSTALL_DIR/bin" || {
        print_error "Failed to extract miren package"
        exit 1
    }
    
    # Create symlink
    sudo ln -sf "$INSTALL_DIR/bin/miren" "/usr/local/bin/miren" || {
        print_error "Failed to create symlink. You may need to add $INSTALL_DIR/bin to your PATH manually."
    }
    
    rm /tmp/miren.tar.gz
    print_success "Miren installed successfully!"
}

install_macos() {
    print_info "Installing Miren for macOS..."
    
    # Check for Docker
    if ! check_docker; then
        print_error "Docker is not installed or not running."
        echo "Please install Docker Desktop from https://www.docker.com/products/docker-desktop"
        exit 1
    fi
    
    print_success "Docker detected and running"
    
    # Download macOS client binary from asset service
    local arch=$(detect_arch)
    local version="${MIREN_VERSION:-main}"
    local binary_url="https://api.miren.cloud/assets/release/miren/${version}/miren-darwin-${arch}.zip"
    
    print_info "Downloading macOS miren client..."
    curl -fSL --connect-timeout 10 --retry 3 "$binary_url" -o /tmp/miren.zip || {
        print_error "Failed to download macOS miren client"
        exit 1
    }
    
    # Verify checksum if available
    local binary_checksum_url="https://api.miren.cloud/assets/release/miren/${version}/miren-darwin-${arch}.zip.sha256"
    verify_checksum "/tmp/miren.zip" "$binary_checksum_url"
    
    # Extract the binary
    print_info "Extracting miren binary..."
    if ! command -v unzip &>/dev/null; then
        print_error "Missing 'unzip'. Please install it (e.g., 'brew install unzip') and rerun."
        exit 1
    fi
    unzip -j /tmp/miren.zip -d /tmp/ || {
        print_error "Failed to extract miren binary"
        exit 1
    }
    
    # Create installation directory
    mkdir -p "$INSTALL_DIR/bin"
    
    # Install the binary
    mv /tmp/miren "$INSTALL_DIR/bin/miren"
    chmod +x "$INSTALL_DIR/bin/miren"
    
    # Cleanup
    rm /tmp/miren.zip
    
    # Download docker-compose configuration from asset service
    print_info "Downloading miren server configuration..."
    curl -fSL --connect-timeout 10 --retry 3 "https://api.miren.cloud/assets/docker-compose.yml" -o /tmp/docker-compose.yml || {
        print_error "Failed to download docker-compose configuration"
        exit 1
    }
    
    # Verify checksum if available
    verify_checksum "/tmp/docker-compose.yml" "https://api.miren.cloud/assets/docker-compose.yml.sha256"
    
    # Start services in background
    print_info "Starting miren server containers..."
    if command -v docker &>/dev/null && docker compose version &>/dev/null; then
        docker compose -f /tmp/docker-compose.yml up -d
    else
        docker-compose -f /tmp/docker-compose.yml up -d
    fi || {
        print_error "Failed to start miren server containers"
        exit 1
    }
    
    rm /tmp/docker-compose.yml
    
    # Wait for miren server to generate client config
    print_info "Waiting for miren server to initialize..."
    sleep 10  # Initial delay to allow container to fully start
    local retries=30
    while [ $retries -gt 0 ]; do
        # Check if file exists, has content, and contains actual config data
        if docker exec miren test -f /tmp/clientconfig.yaml; then
            local file_size=$(docker exec miren wc -c /tmp/clientconfig.yaml 2>/dev/null | awk '{print $1}' || echo "0")
            local has_config=$(docker exec miren grep -q "active_cluster" /tmp/clientconfig.yaml 2>/dev/null && echo "yes" || echo "no")
            
            if [ "$file_size" -gt 100 ] && [ "$has_config" = "yes" ]; then
                print_info "Client configuration ready (${file_size} bytes)"
                break
            else
                print_info "Waiting for configuration content... (${file_size} bytes, attempts remaining: $retries)"
            fi
        else
            print_info "Waiting for configuration file... (attempts remaining: $retries)"
        fi
        sleep 2
        retries=$((retries - 1))
    done
    
    if [ $retries -eq 0 ]; then
        print_error "Miren server failed to generate client configuration"
        exit 1
    fi
    
    # Copy client configuration from container accessible location
    print_info "Configuring miren client..."
    mkdir -p "$HOME/.config/miren"
    docker exec miren cat /tmp/clientconfig.yaml > "$HOME/.config/miren/clientconfig.yaml" || {
        print_error "Failed to copy client configuration"
        exit 1
    }
    
    # Verify the copied config file
    if [ ! -s "$HOME/.config/miren/clientconfig.yaml" ]; then
        print_error "Copied configuration file is empty"
        exit 1
    fi
    
    local copied_size=$(wc -c < "$HOME/.config/miren/clientconfig.yaml" 2>/dev/null || echo "0")
    print_info "Successfully copied client configuration (${copied_size} bytes)"
    
    # Create symlink or add to PATH
    if [ -w "/usr/local/bin" ]; then
        ln -sf "$INSTALL_DIR/bin/miren" "/usr/local/bin/miren"
        print_success "Miren installed to /usr/local/bin/miren"
    else
        print_info "Adding $INSTALL_DIR/bin to PATH..."
        
        # Detect shell and update appropriate config file
        if [[ "$SHELL" == *"zsh"* ]]; then
            echo "export PATH=\"$INSTALL_DIR/bin:\$PATH\"" >> "$HOME/.zshrc"
            print_info "Added to ~/.zshrc. Run 'source ~/.zshrc' to update your current session."
        elif [[ "$SHELL" == *"bash"* ]]; then
            echo "export PATH=\"$INSTALL_DIR/bin:\$PATH\"" >> "$HOME/.bashrc"
            print_info "Added to ~/.bashrc. Run 'source ~/.bashrc' to update your current session."
        else
            print_info "Please add $INSTALL_DIR/bin to your PATH manually."
        fi
    fi
    
    print_success "Miren installed successfully!"
    print_info "You can now use 'miren' command to interact with Miren."
}

# Main installation flow
main() {
    echo "Miren Installer"
    echo "======================"
    echo
    
    # Detect OS
    local os=$(detect_os)
    local arch=$(detect_arch)
    
    print_info "Detected OS: $os"
    print_info "Detected architecture: $arch"
    
    if [[ "$os" == "unsupported" ]] || [[ "$arch" == "unsupported" ]]; then
        print_error "Unsupported OS or architecture"
        exit 1
    fi
    
    # Install based on OS
    case "$os" in
        linux)
            install_linux
            ;;
        macos)
            install_macos
            ;;
        *)
            print_error "Unsupported operating system"
            exit 1
            ;;
    esac
    
    echo
    print_success "Installation complete!"
    echo
    echo "Next steps:"
    echo "  1. Run 'miren init' to initialize a new application"
    echo "  2. Run 'miren deploy' to deploy your application"
    echo "  3. Run 'miren --help' for more information"
}

# Run main function
main "$@"