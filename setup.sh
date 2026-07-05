#!/bin/bash
set -e
go build -o diaryctl .
mkdir -p ~/.local/bin
cp diaryctl ~/.local/bin/diaryctl
echo "✓ diaryctl installed to ~/.local/bin/diaryctl"
