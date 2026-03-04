# Agent Calling & Execution Flow

How Klaudia goes from `node dist/cli.js -p "prompt"` to streaming tool results.

---

## 1. Entry Point

**File:** `08-entry.js:290` — `cliMain()` (exported as `main`)

```
cliMain()
├── Detect client type (CLI, SDK-TS, SDK-Python, etc.)
├── Detect headless mode (-p, --init-only, non-TTY)
│   └── If headless: Ba() disables interactive rendering
├── loadManagedSettings()
├── setupCommander() — Commander CLI parser setup
│   ├── Parse all CLI options (model, permissions, tools, etc.)
│   └── Route to action handler
└── Action handler
    ├── Load API key, validate auth
    ├── Load MCP servers
    ├── Filter tools (hPq)
    └── Dispatch to headless or interactive mode
```

### Key CLI Options

| Flag | Variable | Purpose |
|------|----------|---------|
| `-p, --print` | `z` (headless) | Non-interactive mode, output to stdout |
| `--model` | `gA` | Model alias or full ID |
| `--permission-mode` | `S6.mode` | One of 5 permission modes |
| `--dangerously-skip-permissions` | → `bypassPermissions` mode | Skip all permission checks |
| `--output-format` | `h` | `text` (default), `json`, `stream-json` |
| `--allowedTools` | `X` | Whitelist specific tools |
| `--disallowedTools` | `M` | Blacklist specific tools |
| `-r, --resume` | `V` | Resume session by ID |
| `--continue` | | Resume most recent session |
| `--system-prompt` | `O6` | Override system prompt |
| `--max-turns` | `$.maxTurns` | Limit agentic loop iterations |

---

## 2. Mode Dispatch

**File:** `08-entry.js:1389-1430`

### Headless Mode (`-p` / `--print`)

```javascript
let { runHeadless } = await import("07-app-features.js");  // headlessExports
runHeadless(prompt, getAppState, setAppState, skills, mcpClients, mcpCommands, agents, {
  outputFormat,   // "text" | "json" | "stream-json"
  inputFormat,    // "text" | "stream-json"
  maxTurns,
  maxBudgetUsd,
  // ...
});
```

→ `runHeadless()` at `07-app-features.js:35374`

### Interactive Mode (REPL)

```javascript
let { App } = await import("07-app-features.js");  // appExports → AppComponent
// Renders Ink React tree: App → AppState → FpsMetrics → children
```

→ Renders `AppComponent()` at `07-app-features.js:37645`

---

## 3. Headless Execution (`runHeadless`)

**File:** `07-app-features.js:35374`

```
runHeadless(prompt, getAppState, setAppState, ...)
├── buildOutputWriter() based on outputFormat
├── loadSessionHistory() if resuming
├── headlessAgentLoop() — enter main agentic loop
│   └── Yields events: assistant messages, tool_use, progress
├── Collect final result
└── Output based on format:
    ├── "text"        → print result string
    ├── "json"        → print single JSON object with metadata
    └── "stream-json" → already streamed during loop
```

---

## 4. Main Agentic Loop (`agentLoop`)

**File:** `06-app-ui.js:37758`

This is the core loop that implements the `stream → execute tools → repeat` cycle.

```
agentLoop({ messages, tools, systemPrompt, canUseTool, maxTurns, ... })
│
├── Initialize turn state (turnCount=1, messages, etc.)
│
└── while (true):
    │
    ├── 1. COMPACTION CHECK
    │   ├── microcompact() — inline summaries of old tool results
    │   └── autocompact() — full message compression when approaching token limits
    │
    ├── 2. CALL MODEL
    │   └── callModel() → streamApiRequest()
    │       ├── Build tool schemas (buildToolSchema for each tool)
    │       ├── Merge local + server-side tools
    │       │   // Klaudia: TOOL MERGE POINT
    │       │   h = [...N, ...(w.extraToolSchemas ?? [])]
    │       ├── Build API request (system prompt, messages, tools, thinking config)
    │       └── Stream response from Anthropic API
    │
    ├── 3. PROCESS STREAMED RESPONSE
    │   ├── Yield assistant message events
    │   ├── Extract tool_use blocks from response
    │   └── If streaming tool execution enabled:
    │       └── Start executing tools concurrently while model streams
    │
    ├── 4. TOOL DISPATCH
    │   ├── For each tool_use block:
    │   │   └── dispatchToolUse() — tool executor
    │   │       ├── Look up tool by name (q5)
    │   │       ├── Check permissions (checkToolPermissions)
    │   │       ├── Validate input against Zod schema
    │   │       ├── Call tool.execute()
    │   │       └── Format tool_result message
    │   └── Yield tool_result messages
    │
    ├── 5. CHECK CONTINUATION
    │   ├── No tool_use blocks? → return { reason: "turn_complete" }
    │   ├── Max turns reached? → return { reason: "max_turns" }
    │   ├── Abort signal? → return
    │   └── Otherwise → append messages, increment turn, continue loop
    │
    └── 6. RESPONSE DISCRIMINATION
        // Klaudia: TOOL RESPONSE DISCRIMINATION
        ├── server_tool_use → no local execution (API handles it)
        └── tool_use → dispatch to local tool executor (dispatchToolUse)
```

### Streaming Tool Execution

When `streamingToolExecution` gate is enabled (default), tools begin executing
while the model is still streaming. The streaming executor (`StreamingToolExecutor`) collects
`tool_use` blocks as they arrive and dispatches them immediately:

```
Model streaming: [text...] [tool_use: Read] [text...] [tool_use: Grep]
                              ↓                         ↓
                        Execute Read              Execute Grep (concurrent)
                              ↓                         ↓
                        tool_result               tool_result
```

Results are yielded back into the loop as they complete.

---

## 5. Model Call Chain

### `callModel`
**File:** `06-app-ui.js:105317`

Thin wrapper around `streamApiRequest` with error handling/logging:

```javascript
async function* callModel({ messages, systemPrompt, thinkingConfig, tools, signal, options }) {
  return yield* lW8(messages, async function* () {
    yield* streamApiRequest(messages, systemPrompt, thinkingConfig, tools, signal, options);
  });
}
```

### `streamApiRequest`
**File:** `06-app-ui.js:105407` — `// Klaudia: MAIN API REQUEST`

```
streamApiRequest(messages, systemPrompt, thinkingConfig, tools, signal, options)
│
├── 1. Get model-specific config (RA1) — betas, features, limits
│
├── 2. Check if tools should be deferred (zQ6)
│   └── Deferred tools: loaded on-demand, not sent in initial request
│
├── 3. Filter tools
│   ├── Remove deferred tools (unless pre-approved)
│   └── Apply permission-based filtering
│
├── 4. Build tool schemas
│   └── buildToolSchema() for each tool → { name, description, input_schema }
│
├── 5. Merge server-side tools
│   // Klaudia: TOOL MERGE POINT
│   h = [...N, ...(w.extraToolSchemas ?? [])]
│   //    ^local    ^server-side (web_search, web_fetch, code_execution)
│
├── 6. Build full API request
│   ├── model, system prompt (with cache_control)
│   ├── messages (with cache_control on recent)
│   ├── tools array
│   ├── thinking config (budget_tokens, type)
│   └── betas header
│
└── 7. Stream API response
    └── for await (event of zWq(...)) { yield event }
```

### `buildToolSchema`
**File:** `06-app-ui.js:104778`

Converts a tool object into the API schema format:

```javascript
{
  name: tool.name,
  description: await tool.prompt({ tools, agents, ... }),  // Dynamic description
  input_schema: tool.inputJSONSchema || zodToJsonSchema(tool.inputSchema),
  // Optional:
  input_examples: [...],  // If beta enabled
  defer_loading: true,    // If deferred
  cache_control: {...},   // If caching
}
```

---

## 6. Tool Dispatch (`dispatchToolUse`)

**File:** `06-app-ui.js:36317`

```
dispatchToolUse(toolUseBlock, assistantMessage, canUseTool, toolUseContext)
│
├── 1. LOOKUP
│   └── q5(tools, toolName) — find tool by name
│       └── If not found: yield error tool_result ("No such tool available")
│
├── 2. PERMISSION CHECK (via checkToolPermissions)
│   ├── Check pre-approved rules (allow list)
│   ├── Check denied rules (deny list)
│   ├── Check tool-specific checkPermissions()
│   └── Apply permission mode logic (see §7)
│
├── 3. INPUT VALIDATION
│   ├── Zod schema parse (safeParse)
│   │   └── If fails: return validation error
│   └── Tool-specific validateInput() if exists
│       └── If fails: return validation error
│
├── 4. EXECUTE
│   └── tool.execute(parsedInput, { getAppState, abortController, ... })
│       └── Returns: array of { message: tool_result }
│
└── 5. FORMAT RESULT
    └── Yield tool_result messages back to agentic loop
```

---

## 7. Permission Modes

**File:** `04-react-ink.js:9`

```javascript
fa = ["acceptEdits", "bypassPermissions", "default", "dontAsk", "plan"]
```

### Mode Behavior

| Mode | Description | Tool Gating |
|------|-------------|-------------|
| `default` | Standard interactive | Prompts for dangerous operations |
| `acceptEdits` | Auto-accept edits | File edits allowed, other dangerous ops prompt |
| `bypassPermissions` | Skip all checks | All tools allowed (requires `--dangerously-skip-permissions`) |
| `plan` | Planning only | Tools simulated/blocked, requires approval gate to execute |
| `dontAsk` | Non-interactive deny | Denies anything not pre-approved |

### Permission Check Flow (`checkToolPermissions`)

**File:** `07-app-features.js:1397`

```
checkToolPermissions(tool, input, toolUseContext)
│
├── 1. Check allow rules → { behavior: "allow" }
├── 2. Check deny rules → { behavior: "deny" }
├── 3. Check ask rules → { behavior: "ask" }
├── 4. Call tool.checkPermissions(input) → tool-specific logic
│
└── 5. Apply mode-specific logic:
    ├── bypassPermissions → always allow
    ├── plan + bypass available → allow (simulated)
    ├── dontAsk → deny if not pre-approved
    └── default/acceptEdits → prompt user if "ask"
```

### `--dangerously-skip-permissions`

**File:** `08-entry.js:496-503`

Sets permission mode to `bypassPermissions`. Combined with `-p`, this enables
fully autonomous headless execution:

```bash
node dist/cli.js -p "Create hello.py" --dangerously-skip-permissions
```

---

## 8. Plan Mode

### Entering Plan Mode

Two paths:
1. **CLI flag:** `--permission-mode plan`
2. **Model request:** The model calls the `EnterPlanMode` tool

When plan mode is active:
- Tools are **simulated** — permission checks pass but execution may be restricted
- The model can explore (Read, Glob, Grep) but not modify (Edit, Write, Bash)
- Exiting requires the model to call `ExitPlanMode` tool
- User approval gate before switching to implementation

### Plan Mode in Code

**File:** `08-entry.js:1464`
```javascript
mode: D7() && Lbq().isPlanModeRequired() ? "plan" : S6.mode
```

**File:** `07-app-features.js:1448-1450`
```javascript
if (
  w.toolPermissionContext.mode === "bypassPermissions" ||
  (w.toolPermissionContext.mode === "plan" &&
    w.toolPermissionContext.isBypassPermissionsModeAvailable)
) {
  return { behavior: "allow", ... };
}
```

---

## 9. Context Compaction

**File:** `06-app-ui.js:37808-37862`

Two levels of compaction prevent context window overflow:

### Microcompact (inline summaries)

```javascript
let h = await H.microcompact(messages, context, querySource);
```

Replaces verbose tool results with compact summaries inline. Runs every turn.

### Autocompact (full compression)

```javascript
let { compactionResult } = await H.autocompact(messages, context, {
  systemPrompt, userContext, systemContext, toolUseContext, forkContextMessages,
}, querySource);
```

Triggered when approaching token limits. Compresses the full conversation
history by summarizing older turns while preserving recent context. Uses the
model itself to generate summaries.

**When triggered:**
- Token count approaching model's context window limit
- Message count exceeding configured thresholds
- Explicit request from query source

---

## 10. Sub-Agents / Task Tool

Sub-agents are spawned when the model calls the `Task` tool.

### Spawning Modes

Controlled by `--how-to-spawn-teammates`:
- **In-process** — spawns within the same Node.js process
- **tmux** — spawns in a new tmux pane (for visibility)

### Agent Types

Each sub-agent has a `subagent_type` that determines available tools:
- `general-purpose` — all tools
- `Explore` — read-only tools (Glob, Grep, Read, WebFetch, WebSearch)
- `Plan` — read-only tools, outputs a plan
- `statusline-setup` — Read + Edit only

### Isolation

Optional `isolation: "worktree"` creates a git worktree so the agent works on
an isolated copy of the repository. Changes are returned as a branch.

### Flow

```
Model calls Task tool
├── Parse subagent_type, prompt, options
├── Spawn child agent (in-process or tmux)
│   ├── Child gets own agentic loop (agentLoop)
│   ├── Child gets filtered tool set based on agent type
│   └── Child runs autonomously until complete
├── Collect result
└── Return result as tool_result to parent loop
```

---

## 11. Output Formats

### Text (default)

Final assistant message text printed to stdout:
```bash
node dist/cli.js -p "What is 2+2?"
# → "2 + 2 = 4"
```

### JSON (`--output-format json`)

Single JSON object with full metadata:
```bash
node dist/cli.js -p "What is 2+2?" --output-format json
# → {"result":"2 + 2 = 4","messages":[...],"usage":{...},...}
```

### Stream-JSON (`--output-format stream-json`)

Each event emitted as a JSON line in real-time:
```bash
node dist/cli.js -p "What is 2+2?" --output-format stream-json
# → {"type":"assistant","message":{...}}
# → {"type":"tool_use","name":"Read","input":{...}}
# → {"type":"tool_result",...}
# → {"type":"result","result":"..."}
```

Requires `--verbose` flag.

---

## 12. Session Resume / Continue

### `--resume <session-id>`

**File:** `08-entry.js:618-620, 1641-1874`

Loads a previous session's transcript and resumes the conversation:

```bash
node dist/cli.js --resume abc123
```

1. Validates session ID format (`Tk(V)`)
2. Checks session exists (`Xn6(F8)`)
3. Loads messages from transcript
4. Passes as `initialMessages` to the agentic loop

### `--continue`

Finds and resumes the most recent session in the current directory:

```bash
node dist/cli.js --continue
```

### `--fork-session`

Creates a new session ID when resuming, preserving the original:

```bash
node dist/cli.js --resume abc123 --fork-session
```

---

## 13. System Prompt Construction

**File:** `08-entry.js:887-942, 1165-1203`

The system prompt is built incrementally:

```
1. --system-prompt flag or --system-prompt-file     → base prompt
2. Chrome integration prompt (LU8/vU8)              → prepended if active
3. Agent-specific prompt (BA.getSystemPrompt())      → custom agent instructions
4. --append-system-prompt or --append-system-prompt-file → appended
5. Agent memory prompt                               → "# Custom Agent Instructions"
```

Final prompt passed to `streamApiRequest` with `cache_control` for efficient
token usage across turns.

---

## 14. Model Selection

**File:** `08-entry.js:650-653`, `07-app-features.js:7817-7826`

Resolution order:
1. `--model` CLI flag (alias or full ID)
2. Agent-specific model (`BA.model` if not `"inherit"`)
3. Environment variable `ANTHROPIC_MODEL`
4. Settings file model
5. Default (determined by API)

### Aliases

| Alias | Full Model ID |
|-------|--------------|
| `sonnet` | `claude-sonnet-4-6-20250514` |
| `opus` | `claude-opus-4-6-20250514` |
| `haiku` | `claude-haiku-4-5-20251001` |

---

## 15. SDK / Programmatic API

**File:** `06-app-ui.js:105317` (`callModel`), `07-app-features.js:37644` (`AppComponent`)

The SDK exports enable programmatic usage from TypeScript/Python:

```javascript
// Exports from 07-app-features.js (appExports)
{ App: AppComponent }  // Main React component for interactive mode

// Exports from 08-entry.js
{
  main: cliMain,               // CLI entry point
  showSetupScreens,            // Onboarding flow
  completeOnboarding,          // Mark onboarding done
  startDeferredPrefetches: Fr8,  // Background prefetch
}
```

`callModel()` is the core programmatic API — it takes messages, system prompt,
tools, and options, and returns an async generator of streaming events.

---

## Complete Example Flow

```
$ node dist/cli.js -p "Create hello.py" --dangerously-skip-permissions

1. cliMain() → detect headless mode (has -p)
2. setupCommander() → parse CLI: prompt="Create hello.py", mode=bypassPermissions
3. Load API key, MCP servers, filter tools
4. runHeadless() → headless executor
5. headlessAgentLoop() → enter agentic loop

   Turn 1:
   ├── agentLoop() → callModel() → streamApiRequest()
   │   ├── Build tool schemas (Read, Write, Bash, Glob, Grep, Edit, ...)
   │   ├── Merge server-side tools (WebSearch, WebFetch)
   │   ├── Send to API: messages=[{role:"user", content:"Create hello.py"}]
   │   └── Stream response
   ├── Model responds: tool_use[Write, {path:"hello.py", content:"print..."}]
   ├── dispatchToolUse() → dispatch Write tool
   │   ├── checkToolPermissions() → bypassPermissions → allow
   │   ├── Validate input → ok
   │   └── Execute → write file
   └── tool_result → success

   Turn 2:
   ├── agentLoop() → callModel() → streamApiRequest()
   │   └── Send: messages=[user, assistant(tool_use), user(tool_result)]
   ├── Model responds: text "I've created hello.py..."
   └── No tool_use → loop ends

6. Output: "I've created hello.py with a simple print statement."
```
