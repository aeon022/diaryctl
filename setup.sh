#!/bin/bash
set -e
go build -o diaryctl .
mkdir -p ~/.local/bin
# mv, not cp: cp overwrites in place and leaves macOS's code-signature
# cache stale, causing the next launch to be silently SIGKILLed.
mv diaryctl ~/.local/bin/diaryctl
echo "✓ diaryctl installed to ~/.local/bin/diaryctl"
