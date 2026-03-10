#!/usr/bin/env bash
set -e

# Get the latest tag
LATEST_TAG=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
echo "Latest tag: $LATEST_TAG"

# Parse version components
VERSION=${LATEST_TAG#v}
IFS='.' read -r MAJOR MINOR PATCH <<< "$VERSION"

# Determine bump type (default: patch)
BUMP_TYPE=${1:-patch}

case $BUMP_TYPE in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch)
    PATCH=$((PATCH + 1))
    ;;
  *)
    echo "Usage: $0 [major|minor|patch]"
    exit 1
    ;;
esac

NEW_TAG="v${MAJOR}.${MINOR}.${PATCH}"
echo "New tag: $NEW_TAG"

# Confirm
read -p "Create and push tag $NEW_TAG? [y/N] " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
  echo "Aborted"
  exit 1
fi

# Create and push tag
git tag "$NEW_TAG"
git push origin "$NEW_TAG"

echo "Tag $NEW_TAG pushed. Waiting for release to build..."

# Wait for release
for i in {1..60}; do
  if gh release view "$NEW_TAG" &>/dev/null; then
    echo "Release $NEW_TAG is ready!"
    gh release view "$NEW_TAG"
    exit 0
  fi
  echo -n "."
  sleep 5
done

echo "Timeout waiting for release. Check GitHub Actions."
exit 1
