# GitHub MCP Server CLI Extension

A GitHub CLI extension providing simplified installation and authentication for GitHub MCP Server.

## Installation

```bash
gh extension install MayorFaj/gh-github-mcp-server
```

## Usage

Start the GitHub MCP Server with stdio mode:

### VS Code Integration

Add the following to your VS Code User Settings (JSON):

```json
{
  "mcp": {
    "servers": {
      "github": {
        "command": "gh",
        "args": [
          "github-mcp-server",
          "stdio"
        ]
      }
    }
  }
}
```

You can edit settings by pressing Ctrl+Shift+P (or Cmd+Shift+P on macOS) and typing "Preferences: Open User Settings (JSON)".

### Features

- Zero Configuration: Uses your existing GitHub CLI authentication
- Automatic Installation: Downloads the correct binary for your platform
- No Additional Dependencies: No Docker or manual token management required

### License

MIT License
