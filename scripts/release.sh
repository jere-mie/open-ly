#!/bin/bash

# Check if GitHub CLI is installed
if ! command -v gh &> /dev/null; then
    echo "GitHub CLI (gh) is not installed. Please install it first."
    exit 1
fi

# Check if version.txt exists
if [ ! -f "version.txt" ]; then
    echo "version.txt not found. Please create it with the release version."
    exit 1
fi

# Read the tag from version.txt
tag=$(cat version.txt | tr -d '[:space:]')
release_name="Release $tag"

echo "Creating GitHub release for tag: $tag"

# Create the release
gh release create "$tag" bin/* --title "$release_name" --notes "Automated release for $tag"

if [ $? -eq 0 ]; then
    echo "GitHub release created successfully."
else
    echo "Failed to create GitHub release."
    exit 1
fi