#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Default values
INSTALL_DIR="$HOME/.miren/runtime"
BINARY_NAME="runtime"
OCI_IMAGE="oci.miren.cloud/runtime:latest"

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

install_linux() {
    print_info "Installing Miren Runtime for Linux..."
    
    # Download release package
    local arch=$(detect_arch)
    local download_url="https://github.com/mirendev/runtime/releases/latest/download/runtime-linux-${arch}.tar.gz"
    
    print_info "Downloading runtime package..."
    curl -L "$download_url" -o /tmp/runtime.tar.gz || {
        print_error "Failed to download runtime package"
        exit 1
    }
    
    # Create installation directory
    mkdir -p "$INSTALL_DIR/bin"
    
    # Extract package
    print_info "Extracting runtime components..."
    tar -xzf /tmp/runtime.tar.gz -C "$INSTALL_DIR/bin" || {
        print_error "Failed to extract runtime package"
        exit 1
    }
    
    # Create symlink
    sudo ln -sf "$INSTALL_DIR/bin/runtime" "/usr/local/bin/runtime" || {
        print_error "Failed to create symlink. You may need to add $INSTALL_DIR/bin to your PATH manually."
    }
    
    rm /tmp/runtime.tar.gz
    print_success "Runtime installed successfully!"
}

install_macos() {
    print_info "Installing Miren Runtime for macOS..."
    
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
    local binary_url="https://api.miren.cloud/assets/release/runtime/${version}/runtime-darwin-${arch}.zip"
    
    print_info "Downloading macOS runtime client..."
    curl -L "$binary_url" -o /tmp/runtime.zip || {
        print_error "Failed to download macOS runtime client"
        exit 1
    }
    
    # Extract the binary
    print_info "Extracting runtime binary..."
    unzip -j /tmp/runtime.zip -d /tmp/ || {
        print_error "Failed to extract runtime binary"
        exit 1
    }
    
    # Create installation directory
    mkdir -p "$INSTALL_DIR/bin"
    
    # Install the binary
    mv /tmp/runtime "$INSTALL_DIR/bin/runtime"
    chmod +x "$INSTALL_DIR/bin/runtime"
    
    # Cleanup
    rm /tmp/runtime.zip
    
    # Download docker-compose configuration from asset service
    print_info "Downloading runtime server configuration..."
    curl -L "https://api.miren.cloud/assets/docker-compose.yml" -o /tmp/docker-compose.yml || {
        print_error "Failed to download docker-compose configuration"
        exit 1
    }
    
    # Start services in background
    print_info "Starting runtime server containers..."
    docker compose -f /tmp/docker-compose.yml up -d || {
        print_error "Failed to start runtime server containers"
        exit 1
    }
    
    rm /tmp/docker-compose.yml
    
    # Wait for runtime server to generate client config
    print_info "Waiting for runtime server to initialize..."
    sleep 10  # Initial delay to allow container to fully start
    local retries=30
    while [ $retries -gt 0 ]; do
        # Check if file exists, has content, and contains actual config data (check both locations)
        if docker exec miren-runtime test -f /tmp/clientconfig.yaml; then
            local file_size=$(docker exec miren-runtime wc -c /tmp/clientconfig.yaml 2>/dev/null | awk '{print $1}' || echo "0")
            local has_config=$(docker exec miren-runtime grep -q "active_cluster" /tmp/clientconfig.yaml 2>/dev/null && echo "yes" || echo "no")
            
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
        print_error "Runtime server failed to generate client configuration"
        exit 1
    fi
    
    # Copy client configuration from container accessible location
    print_info "Configuring runtime client..."
    mkdir -p "$HOME/.config/runtime"
    docker exec miren-runtime cat /tmp/clientconfig.yaml > "$HOME/.config/runtime/clientconfig.yaml" || {
        print_error "Failed to copy client configuration"
        exit 1
    }
    
    # Verify the copied config file
    if [ ! -s "$HOME/.config/runtime/clientconfig.yaml" ]; then
        print_error "Copied configuration file is empty"
        exit 1
    fi
    
    local copied_size=$(wc -c < "$HOME/.config/runtime/clientconfig.yaml" 2>/dev/null || echo "0")
    print_info "Successfully copied client configuration (${copied_size} bytes)"
    
    # Create symlink or add to PATH
    if [ -w "/usr/local/bin" ]; then
        ln -sf "$INSTALL_DIR/bin/runtime" "/usr/local/bin/runtime"
        print_success "Runtime wrapper installed to /usr/local/bin/runtime"
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
    
    print_success "Runtime installed successfully!"
    print_info "You can now use 'runtime' command to interact with Miren Runtime."
}

# Main installation flow
main() {
    echo "Miren Runtime Installer"
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
    echo "  1. Run 'runtime init' to initialize a new application"
    echo "  2. Run 'runtime deploy' to deploy your application"
    echo "  3. Run 'runtime --help' for more information"
}

# Run main function
main "$@"