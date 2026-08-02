#!/bin/bash
set -e
go build -o diaryctl .
mkdir -p ~/.local/bin
# mv, not cp: cp overwrites in place and leaves macOS's code-signature
# cache stale, causing the next launch to be silently SIGKILLed.
mv diaryctl ~/.local/bin/diaryctl
echo "✓ diaryctl installed to ~/.local/bin/diaryctl"

# ── MCP: register in ~/.claude.json ───────────────────────────────────────────
CLAUDE_JSON="$HOME/.claude.json"
if command -v python3 &>/dev/null; then
  python3 - "$CLAUDE_JSON" "$HOME/.local/bin/diaryctl" <<'PYEOF'
import json, sys, os

claude_json = sys.argv[1]
binary_path = sys.argv[2]

data = {}
if os.path.exists(claude_json):
    with open(claude_json) as f:
        try:
            data = json.load(f)
        except Exception:
            pass

data.setdefault("mcpServers", {})
data["mcpServers"]["diaryctl"] = {
    "command": binary_path,
    "args": ["mcp"]
}

with open(claude_json, "w") as f:
    json.dump(data, f, indent=2)
    f.write("\n")

print("✓ MCP server registered in ~/.claude.json")
print("  Restart Claude Code to activate diaryctl MCP tools")
PYEOF
else
  echo "  To enable MCP (Claude Code integration), add to ~/.claude.json:"
  echo "  \"mcpServers\": { \"diaryctl\": { \"command\": \"$HOME/.local/bin/diaryctl\", \"args\": [\"mcp\"] } }"
fi
