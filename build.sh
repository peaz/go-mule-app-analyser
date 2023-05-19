#!/bin/bash

# Get the latest version from Git tag
version=$(git describe --abbrev=0 --tags)

directory="releases/$version"
if [ ! -d "$directory" ]; then
    mkdir "$directory"
    echo "Directory created: $directory"
else
    echo "Directory already exists: $directory"
fi

cp -r src/mule-app-analyser/resources $directory/resources

env GOPATH=$PWD GOOS=linux GOARCH=amd64 go build -o=$directory/mule-app-analyser-$version-linux src/mule-app-analyser/main.go
env GOPATH=$PWD GOOS=darwin GOARCH=amd64 go build -o=$directory/mule-app-analyser-$version-osx src/mule-app-analyser/main.go
env GOPATH=$PWD GOOS=windows GOARCH=amd64 go build -o=$directory/mule-app-analyser-$version-win.exe src/mule-app-analyser/main.go