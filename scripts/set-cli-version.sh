#!/bin/bash

# Check if a version number was provided as an argument
if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <new-version>"
    exit 1
fi

# Assign the new version number from command line arguments
NEW_VERSION=$1
CLI_ROOT_GO_FILE="./modules/cli/cmd/root.go"

# Check if the file exists
if [ ! -f "$CLI_ROOT_GO_FILE" ]; then
    echo "Error: File $CLI_ROOT_GO_FILE not found."
    exit 1
fi

# Update the Version field in the root.go file
sed -i.bak -E "s/(Version: )\"[^\"]*\"/\1\"$NEW_VERSION\"/" "$CLI_ROOT_GO_FILE" && rm "$CLI_ROOT_GO_FILE.bak"

echo "Version in $CLI_ROOT_GO_FILE updated to '$NEW_VERSION'."
