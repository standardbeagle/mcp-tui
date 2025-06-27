#!/bin/bash

# Build release binaries for multiple platforms

VERSION=${VERSION:-$(grep 'version = ' main.go | cut -d'"' -f2)}
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

echo "Building mcp-tui v$VERSION"
echo "Build date: $BUILD_DATE"
echo "Git commit: $GIT_COMMIT"

# Create dist directory
mkdir -p dist

# Build flags for version info
LDFLAGS="-X main.version=$VERSION -X main.buildDate=$BUILD_DATE -X main.gitCommit=$GIT_COMMIT"

# Function to build for a specific platform
build_platform() {
    local GOOS=$1
    local GOARCH=$2
    local OUTPUT=$3
    
    echo "Building for $GOOS/$GOARCH..."
    GOOS=$GOOS GOARCH=$GOARCH go build -ldflags "$LDFLAGS" -o "dist/$OUTPUT" .
    
    if [ $? -eq 0 ]; then
        echo "✓ Built dist/$OUTPUT"
        # Create tar.gz archive
        cd dist
        tar -czf "${OUTPUT%.exe}.tar.gz" "$(basename $OUTPUT)"
        cd ..
        echo "✓ Created dist/${OUTPUT%.exe}.tar.gz"
    else
        echo "✗ Failed to build for $GOOS/$GOARCH"
    fi
}

# Build for all platforms
build_platform "linux" "amd64" "mcp-tui_${VERSION}_linux_amd64"
build_platform "linux" "arm64" "mcp-tui_${VERSION}_linux_arm64"
build_platform "darwin" "amd64" "mcp-tui_${VERSION}_darwin_amd64"
build_platform "darwin" "arm64" "mcp-tui_${VERSION}_darwin_arm64"
build_platform "windows" "amd64" "mcp-tui_${VERSION}_windows_amd64.exe"

echo ""
echo "Build complete! Binaries are in the dist/ directory."
echo ""
echo "To create a GitHub release:"
echo "1. git tag v$VERSION"
echo "2. git push origin v$VERSION"
echo "3. Create release on GitHub and upload the .tar.gz files from dist/"