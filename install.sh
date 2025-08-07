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
OCI_IMAGE="miren/runtime:latest"

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
    
    # Pull the OCI image
    print_info "Pulling Miren Runtime Docker image..."
    docker pull "$OCI_IMAGE" || {
        print_error "Failed to pull Docker image"
        exit 1
    }
    
    # Create installation directory
    mkdir -p "$INSTALL_DIR/bin"
    
    # Create wrapper script
    print_info "Creating runtime wrapper script..."
    cat > "$INSTALL_DIR/bin/runtime" << 'EOF'
#!/bin/bash
# Miren Runtime wrapper for macOS (Docker-based)

# Runtime configuration
MIREN_DOCKER_IMAGE="${MIREN_DOCKER_IMAGE:-miren/runtime:latest}"
MIREN_DATA_DIR="${MIREN_DATA_DIR:-$HOME/.miren/data}"
MIREN_CONFIG_DIR="${MIREN_CONFIG_DIR:-$HOME/.config/runtime}"

# Ensure directories exist
mkdir -p "$MIREN_DATA_DIR" "$MIREN_CONFIG_DIR"

# Function to check if runtime container is running
is_runtime_running() {
    docker ps --format '{{.Names}}' | grep -q '^miren-runtime$'
}

# Function to start runtime container
start_runtime() {
    if ! is_runtime_running; then
        echo "Starting Miren Runtime container..."
        docker run -d \
            --name miren-runtime \
            --privileged \
            --pid host \
            --network host \
            -v "$MIREN_DATA_DIR:/data" \
            -v "$MIREN_CONFIG_DIR:/root/.config/runtime" \
            -v "$PWD:/workspace" \
            -w /workspace \
            "$MIREN_DOCKER_IMAGE" \
            tail -f /dev/null
        
        # Wait for container to be ready
        sleep 2
    fi
}

# Start runtime if needed
start_runtime

# Execute command in container
docker exec -it \
    -e TERM="$TERM" \
    -e HOME="/root" \
    -w "$PWD" \
    miren-runtime \
    runtime "$@"
EOF
    
    chmod +x "$INSTALL_DIR/bin/runtime"
    
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