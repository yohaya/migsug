#!/bin/bash

# Script to automatically bump version in migsug.sh
# Usage: ./bump-version.sh [major|minor|patch]

SCRIPT_FILE="migsug.sh"
BUMP_TYPE="${1:-patch}"  # Default to patch if not specified

# Extract current version
CURRENT_VERSION=$(grep "# VERSION:" "$SCRIPT_FILE" | sed 's/# VERSION: //')

if [ -z "$CURRENT_VERSION" ]; then
    echo "Error: Could not find version in $SCRIPT_FILE"
    exit 1
fi

# Split version into parts
IFS='.' read -r major minor patch <<< "$CURRENT_VERSION"

# Bump version based on type
case $BUMP_TYPE in
    major)
        major=$((major + 1))
        minor=0
        patch=0
        ;;
    minor)
        minor=$((minor + 1))
        patch=0
        ;;
    patch)
        patch=$((patch + 1))
        ;;
    *)
        echo "Error: Invalid bump type. Use major, minor, or patch"
        exit 1
        ;;
esac

NEW_VERSION="$major.$minor.$patch"
CURRENT_DATE=$(date +%Y-%m-%d)

# Update version in script
sed -i.bak "s/# VERSION: .*/# VERSION: $NEW_VERSION/" "$SCRIPT_FILE"
sed -i.bak "s/# Last updated: .*/# Last updated: $CURRENT_DATE/" "$SCRIPT_FILE"
rm -f "${SCRIPT_FILE}.bak"

echo "Version bumped from $CURRENT_VERSION to $NEW_VERSION"
echo "$NEW_VERSION"
