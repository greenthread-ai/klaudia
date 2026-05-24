# Anthropic Server-Side Tools (reference)

Tools the Anthropic Messages API executes server-side: sent in the `tools` array
of a request, with results streamed back as special content-block types.

This is a wire-format reference for the Anthropic provider. Klaudia ships its own
**local** web search/fetch/browse tools (lazy headless Chrome — see the README),
so it does not depend on these. On the Anthropic provider it can still enable the
server-side `web_search` / `web_fetch` betas (`internal/agent/webtools.go`); the
shapes below document what those return.

---

## Web Search

**Tool types:** `web_search_20260209` (current), `web_search_20250305` (legacy)

The `_20260209` version supports dynamic filtering (model can refine domains per-call).

### Tool Definition (sent to API)

```json
{
  "type": "web_search_20260209",
  "name": "web_search",
  "max_uses": 8,
  "allowed_domains": ["docs.example.com"],
  "blocked_domains": ["spam.com"]
}
```

`allowed_domains` and `blocked_domains` are optional, mutually exclusive at the
tool-definition level. The model can also specify them per-call (dynamic filtering).

### Model Input (what Claude sends)

```json
{
  "query": "search terms here",
  "allowed_domains": ["example.com"],
  "blocked_domains": ["spam.com"]
}
```

- `query`: string, min 2 chars, no wildcards (`*`, `?`)
- `allowed_domains`: optional string array
- `blocked_domains`: optional string array
- Cannot specify both domain filters in same call

### Response

Content block type: `web_search_tool_result`

**Success:**
```json
{
  "type": "web_search_tool_result",
  "tool_use_id": "toolu_...",
  "content": [
    { "title": "Page Title", "url": "https://example.com/page" },
    { "title": "Another Result", "url": "https://example.com/other" }
  ]
}
```

**Error:**
```json
{
  "type": "web_search_tool_result",
  "tool_use_id": "toolu_...",
  "content": { "error_code": "rate_limited" }
}
```


---

## Web Fetch

**Tool types:** `web_fetch_20260209` (current), `web_fetch_20250305` (legacy)

### Tool Definition

```json
{
  "type": "web_fetch_20260209",
  "name": "web_fetch"
}
```

### Model Input

```json
{
  "url": "https://example.com/page",
  "prompt": "Extract the main content from this page"
}
```

- `url`: string, valid URL (HTTP auto-upgraded to HTTPS)
- `prompt`: string, describes what to extract from the fetched content

### Response

Content block type: `web_fetch_tool_result`

```json
{
  "type": "web_fetch_tool_result",
  "tool_use_id": "toolu_...",
  "content": {
    "url": "https://example.com/page",
    "code": 200,
    "codeText": "OK",
    "bytes": 45230,
    "durationMs": 1250,
    "result": "Extracted content based on the prompt..."
  }
}
```

**Redirect handling:** For 301/307/308, returns redirect target URL and asks
the model to re-fetch with the new URL.


---

## Code Execution

**Tool type:** `code_execution_20260120`

Server-side sandboxed execution. Automatically includes two sub-tools:
`bash_code_execution` and `text_editor_code_execution`.

### Tool Definition

```json
{
  "type": "code_execution_20260120",
  "name": "code_execution"
}
```

Requires beta header: `files-api-2025-04-14`

### Sub-Tool: bash_code_execution

The model writes bash/python code; Anthropic's servers execute it in a sandbox.

**Sandbox environment:**
- Python 3.11 with: pandas, numpy, scipy, scikit-learn, matplotlib, seaborn,
  openpyxl, pillow, pypdf, pdfplumber, sympy, and more
- 1 CPU, 5 GiB RAM, 5 GiB disk
- No internet access
- Containers persist 30 days

**Response type:** `bash_code_execution_tool_result`

```json
{
  "type": "bash_code_execution_tool_result",
  "tool_use_id": "toolu_...",
  "content": {
    "type": "bash_code_execution_result",
    "return_code": 0,
    "stdout": "output here",
    "stderr": "",
    "content": []
  }
}
```

### Sub-Tool: text_editor_code_execution

Server-side file create/view/edit operations within the sandbox.

**Response type:** `text_editor_code_execution_tool_result`


---

## MCP (Model Context Protocol)

MCP servers provide additional tools dynamically. Klaudia supports both local
MCP servers (stdio) and remote MCP servers (via Anthropic's proxy).

### Remote MCP Proxy

**Proxy URL:** `https://mcp-proxy.anthropic.com/v1/mcp/{server_id}`

Discovery endpoint: `GET https://api.anthropic.com/v1/mcp_servers?limit=1000`

### Tool Naming Convention

MCP tools are prefixed: `mcp__{server_name}__{tool_name}`

Example: `mcp__slack__read_channel`, `mcp__github__create_issue`

### MCP Tool Use

**Request (model calls MCP tool):**
```json
{
  "type": "tool_use",
  "id": "toolu_...",
  "name": "mcp__slack__read_channel",
  "input": { "channel": "#general", "limit": 10 }
}
```

**Response:**
```json
{
  "type": "mcp_tool_result",
  "tool_use_id": "toolu_...",
  "content": [
    { "type": "text", "text": "result data..." }
  ]
}
```

### Browser Automation (MCP-based)

Computer use / browser automation runs through an MCP bridge:

**Service pattern:** `claude-mcp-browser-bridge-*`

**Available browser tools:**
| Tool | Purpose |
|------|---------|
| `computer` | Mouse/keyboard/screenshot interactions |
| `tabs_context_mcp` | Get browser tab context |
| `find` | Find elements on page |
| `read_page` | Read page content |
| `navigate` | Navigate to URL |
| `set_form_value` | Fill form fields |
| `execute_javascript` | Run JS in page |
| `get_accessibility_tree` | A11y tree for the page |
| `read_console` | Browser console output |
| `read_network` | Network request log |
| `upload_image` | Upload image to page |
| `read_text_content` | Extract text from page |
| `create_empty_tab` | Open new tab |
| `read_shortcuts` | Available keyboard shortcuts |
| `resize_window` | Resize browser window |

**Bridge connection:** WebSocket, authenticated via OAuth token or dev user ID.

### MCP Server Config

Local MCP servers are configured in settings:
```json
{
  "mcpServers": {
    "my-server": {
      "command": "npx",
      "args": ["-y", "my-mcp-server"],
      "env": {}
    }
  }
}
```


---

## Common Response Block Types

All content block types that can appear in streamed responses:

| Type | Source | Purpose |
|------|--------|---------|
| `text` | Model | Claude's text response |
| `thinking` | Model | Extended thinking content |
| `tool_use` | Model | Client-side tool call request |
| `server_tool_use` | Server | Server-side tool invocation indicator |
| `web_search_tool_result` | Server | Search results |
| `web_fetch_tool_result` | Server | Fetched page content |
| `bash_code_execution_tool_result` | Server | Bash execution output |
| `text_editor_code_execution_tool_result` | Server | File operation results |
| `code_execution_tool_result` | Server | General code execution results |
| `mcp_tool_use` | Model | MCP tool call |
| `mcp_tool_result` | Server | MCP tool results |
| `tool_search_tool_result` | Server | Tool discovery results |
| `container_upload` | Server | File upload indicator |

## Stop Reasons

| Reason | Meaning |
|--------|---------|
| `end_turn` | Model finished responding |
| `tool_use` | Model wants to call a client-side tool |
| `pause_turn` | Server-side tool loop hit limit (max 10 iterations) — client should re-send to resume |
| `max_tokens` | Hit token limit |

---

## Beta Headers

Server-side features are gated behind beta headers sent as `anthropic-beta`:

| Beta | Purpose |
|------|---------|
| `files-api-2025-04-14` | File ops for code execution |
| `web-search-2025-03-05` | Web search capability |
| `skills-2025-10-02` | Skills feature |
| `adaptive-thinking-2026-01-28` | Extended/adaptive thinking |
| `interleaved-thinking-2025-05-14` | Interleaved thinking mode |
| `structured-outputs-2025-11-13` | Structured output format |
| `ccr-byoc-2025-07-29` | Customer Controlled Regions |

---

## How Klaudia covers these locally

| Server-side tool | Klaudia equivalent |
| --- | --- |
| Web Search | local `WebSearch` (lazy headless Chrome, DDG/Google) — `internal/browser` |
| Web Fetch | local `WebFetch` / `BrowserNavigate` / `BrowserSnapshot` → Markdown |
| MCP | local stdio + HTTP/SSE MCP servers — `internal/mcp` |
| Code Execution | container sandbox for the Bash tool (`sandbox.mode = "container"`) |
| Browser Automation | the local browser tools above; an MCP browser server can be registered for richer control |

The Anthropic server-side `web_search` / `web_fetch` betas remain available when
running on the Anthropic provider (`internal/agent/webtools.go`).
