#!/bin/bash

# Define output directory
OUTPUT_DIR="bin"
mkdir -p "$OUTPUT_DIR"

# Define target platforms and architectures
PLATFORMS=("windows" "linux" "darwin")
ARCHITECTURES=("amd64" "386" "arm64" "arm")

# Build each combination
for OS in "${PLATFORMS[@]}"; do
    for ARCH in "${ARCHITECTURES[@]}"; do
        OUTPUT_NAME="openly_${OS}_${ARCH}"
        
        # Skip windows/arm, darwin/arm and darwin/386
        if [ "$OS" == "windows" ] && [ "$ARCH" == "arm" ]; then
            continue
        elif [ "$OS" == "darwin" ] && [ "$ARCH" == "arm" ]; then
            continue
        elif [ "$OS" == "darwin" ] && [ "$ARCH" == "386" ]; then
            continue
        fi

        # Windows binaries need .exe extension
        if [ "$OS" == "windows" ]; then
            OUTPUT_NAME+=".exe"
        fi
        
        echo "Building for $OS/$ARCH..."
        
        # Set environment variables and build
        env GOOS=$OS GOARCH=$ARCH go build -o "$OUTPUT_DIR/$OUTPUT_NAME" .
        
        if [ $? -ne 0 ]; then
            echo "Failed to build for $OS/$ARCH"
        else
            echo "Successfully built: $OUTPUT_DIR/$OUTPUT_NAME"
        fi
    done
done

echo "All builds completed."
