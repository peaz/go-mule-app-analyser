#!/bin/bash
set -e

# Resolve repo root regardless of where the script is called from
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

version=$(git describe --abbrev=0 --tags)
directory="releases/$version"

if [ ! -d "$directory" ]; then
    mkdir -p "$directory"
    echo "Directory created: $directory"
else
    echo "Directory already exists: $directory"
fi

GOOS=linux   GOARCH=amd64 go build -mod=vendor -o="$directory/mule-app-analyser-$version-linux"        ./cmd/mule-app-analyser/
GOOS=darwin  GOARCH=amd64 go build -mod=vendor -o="$directory/mule-app-analyser-$version-osx-amd64"    ./cmd/mule-app-analyser/
GOOS=darwin  GOARCH=arm64 go build -mod=vendor -o="$directory/mule-app-analyser-$version-osx-arm64"    ./cmd/mule-app-analyser/
GOOS=windows GOARCH=amd64 go build -mod=vendor -o="$directory/mule-app-analyser-$version-win.exe"      ./cmd/mule-app-analyser/

echo "Build complete: $directory"
