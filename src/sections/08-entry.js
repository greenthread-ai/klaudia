var Sbq = {};
s1(Sbq, {
  startDeferredPrefetches: () => Fr8,
  showSetupScreens: () => showSetupScreens,
  main: () => cliMain,
  completeOnboarding: () => completeOnboarding,
});
import { existsSync as ybq, readFileSync as Ebq } from "fs";
import { cwd as qCz } from "process";
import { resolve as DS1 } from "path";
function loadManagedSettings() {
  try {
    let A = getConfigValue("policySettings");
    if (A) {
      let q = MZq(A);
      emitEvent("tengu_managed_settings_loaded", {
        keyCount: q.length,
        keys: q.join(","),
      });
    }
  } catch {}
}
function isDebuggerAttached() {
  let A = gO6(),
    q = process.execArgv.some((Y) => {
      if (A) return /--inspect(-brk)?/.test(Y);
      else return /--inspect(-brk)?|--debug(-brk)?/.test(Y);
    }),
    K =
      process.env.NODE_OPTIONS &&
      /--inspect(-brk)?|--debug(-brk)?/.test(process.env.NODE_OPTIONS);
  try {
    return !!global.require("inspector").url() || q || K;
  } catch {
    return q || K;
  }
}
function completeOnboarding() {
  updateSettings((A) => ({
    ...A,
    hasCompletedOnboarding: !0,
    lastOnboardingVersion: {
      ISSUES_EXPLAINER:
        "report the issue at https://github.com/anthropics/claude-code/issues",
      PACKAGE_URL: "klaudia",
      README_URL: "https://code.claude.com/docs/en/overview",
      VERSION: "2.1.66-klaudia",
      FEEDBACK_CHANNEL: "https://github.com/anthropics/claude-code/issues",
      BUILD_TIME: "2026-03-04T00:18:36Z",
    }.VERSION,
  }));
}
function zCz(A, q) {
  return new Promise((K) => {
    let Y = (z) => void K(z);
    A.render(q(Y));
  });
}
async function c86(A, q, K) {
  let { Text: Y } = await Promise.resolve().then(() => (Q6(), oI6));
  (A.render(r$.default.createElement(Y, { color: "error" }, q)),
    A.unmount(),
    await K?.(),
    process.exit(1));
}
function jp(A, q, K) {
  return zCz(A, (Y) =>
    r$.default.createElement(
      Xj,
      { onChangeAppState: K?.onChangeAppState },
      r$.default.createElement(hD, null, q(Y)),
    ),
  );
}
async function To6(A, q) {
  (A.render(q), Fr8(), await A.waitUntilExit(), await rq(0));
}
async function showSetupScreens(A, q, K, Y, z) {
  if (isTruthy(!1) || process.env.IS_DEMO) return !1;
  let w = getSettings(),
    _ = !1;
  if (!w.theme || !w.hasCompletedOnboarding) {
    _ = !0;
    let [, { Onboarding: $ }] = await Promise.all([
      W16(),
      Promise.resolve().then(() => (lNq(), cNq)),
    ]);
    await jp(
      A,
      (O) =>
        r$.default.createElement($, {
          onDone: () => {
            (completeOnboarding(), O());
          },
        }),
      { onChangeAppState: h86 },
    );
  }
  if (!isTruthy(process.env.CLAUBBIT)) {
    if (!Ew()) {
      let { TrustDialog: O } = await Promise.resolve().then(() => ($Vq(), _Vq));
      await jp(A, (H) =>
        r$.default.createElement(O, { commands: Y, onDone: H }),
      );
    }
    (uk6(!0), ly1(), lF(), ZO());
    let { errors: $ } = al();
    if ($.length === 0) await wNq(A);
    if (await e74()) {
      let O = Sg6(),
        { ClaudeMdExternalIncludesDialog: H } = await Promise.resolve().then(
          () => (rg8(), W9q),
        );
      await jp(A, (j) =>
        r$.default.createElement(H, {
          onDone: j,
          isStandaloneDialog: !0,
          externalIncludes: O,
        }),
      );
    }
  }
  if ((bNq(), S86(), pl8(), await vZ6())) {
    let { GroveDialog: $ } = await Promise.resolve().then(() => (qL1(), KJq));
    if (
      (await jp(A, (H) =>
        r$.default.createElement($, {
          showIfAlreadyViewed: !1,
          location: _ ? "onboarding" : "policy_update_modal",
          onDone: H,
        }),
      )) === "escape"
    )
      return (emitEvent("tengu_grove_policy_exited", {}), _3(0), !1);
  }
  if (process.env.ANTHROPIC_API_KEY && !SZ()) {
    let $ = mV(process.env.ANTHROPIC_API_KEY);
    if (wr6($) === "new") {
      let { ApproveApiKey: H } = await Promise.resolve().then(
        () => (Ni8(), FNq),
      );
      await jp(
        A,
        (j) =>
          r$.default.createElement(H, { customApiKeyTruncated: $, onDone: j }),
        { onChangeAppState: h86 },
      );
    }
  }
  if ((q === "bypassPermissions" || K) && !dW6()) {
    let { BypassPermissionsModeDialog: $ } = await Promise.resolve().then(
      () => (HVq(), OVq),
    );
    await jp(A, (O) => r$.default.createElement($, { onAccept: O }));
  }
  if (z && !getSettings().hasCompletedClaudeInChromeOnboarding) {
    let { ClaudeInChromeOnboarding: $ } = await Promise.resolve().then(
      () => (JVq(), jVq),
    );
    await jp(A, (O) => r$.default.createElement($, { onDone: O }));
  }
  return _;
}
function wCz() {
  (updateSettings((q) => ({ ...q, numStartups: (q.numStartups ?? 0) + 1 })), $Cz());
  let A = O5(Q_6() ?? KW());
  kR1(y1(), fX(A, iH()));
}
function _Cz() {
  let A = {};
  if (process.env.NODE_EXTRA_CA_CERTS) A.has_node_extra_ca_certs = !0;
  if (process.env.CLAUDE_CODE_CLIENT_CERT) A.has_client_cert = !0;
  if (sI1("--use-system-ca")) A.has_use_system_ca = !0;
  if (sI1("--use-openssl-ca")) A.has_use_openssl_ca = !0;
  return A;
}
async function $Cz() {
  let [A, q, K] = await Promise.all([Aj(), qD6(), eTq(y1())]);
  emitEvent("tengu_startup_telemetry", {
    is_git: A,
    worktree_count: q,
    repo_text_file_size_bytes: K ?? void 0,
    sandbox_enabled: xA.isSandboxingEnabled(),
    are_unsandboxed_commands_allowed: xA.areUnsandboxedCommandsAllowed(),
    is_auto_bash_allowed_if_sandbox_enabled:
      xA.isAutoAllowBashIfSandboxedEnabled(),
    auto_updater_disabled: nF(),
    prefers_reduced_motion: U7().prefersReducedMotion ?? !1,
    ..._Cz(),
  });
}
function OCz() {
  (ZNq(), TNq(), VNq(), CNq(), yNq(), ENq(), g$q().catch(() => {}));
}
function HCz() {
  if (C7()) {
    ($8("info", "prefetch_system_context_non_interactive"), ZO());
    return;
  }
  if (Ew()) ($8("info", "prefetch_system_context_has_trust"), ZO());
  else $8("info", "prefetch_system_context_skipped_no_trust");
}
function Fr8() {
  if (isTruthy(process.env.CLAUDE_CODE_EXIT_AFTER_FIRST_RENDER)) return;
  if (
    (kZA(),
    Q_(),
    HCz(),
    LR1(),
    isTruthy(process.env.CLAUDE_CODE_USE_BEDROCK) &&
      !isTruthy(process.env.CLAUDE_CODE_SKIP_BEDROCK_AUTH))
  )
    Tl8();
  if (
    (Z81(y1(), AbortSignal.timeout(3000), []),
    vl8(),
    qH.initialize(),
    !isTruthy(process.env.CLAUDE_CODE_SIMPLE))
  )
    gV6.initialize();
}
function jCz(A) {
  try {
    let q = A.trim(),
      K = q.startsWith("{") && q.endsWith("}"),
      Y;
    if (K) {
      if (!s3(q))
        (process.stderr.write(
          H1.red(`Error: Invalid JSON provided to --settings
`),
        ),
          process.exit(1));
      ((Y = gk1("claude-settings", ".json")), Nz(Y, q, "utf8"));
    } else {
      let { resolvedPath: z } = P$(P1(), A);
      if (!ybq(z))
        (process.stderr.write(
          H1.red(`Error: Settings file not found: ${z}
`),
        ),
          process.exit(1));
      Y = z;
    }
    (JI1(Y), M$());
  } catch (q) {
    if (q instanceof Error) sendError(q);
    (process.stderr.write(
      H1.red(`Error processing settings: ${q instanceof Error ? q.message : String(q)}
`),
    ),
      process.exit(1));
  }
}
function JCz(A) {
  try {
    let q = k_7(A);
    (fI1(q), M$());
  } catch (q) {
    if (q instanceof Error) sendError(q);
    (process.stderr.write(
      H1.red(`Error processing --setting-sources: ${q instanceof Error ? q.message : String(q)}
`),
    ),
      process.exit(1));
  }
}
function DCz() {
  Bq("eagerLoadSettings_start");
  let A = Xi8("--settings");
  if (A) jCz(A);
  let q = Xi8("--setting-sources");
  if (q !== void 0) JCz(q);
  Bq("eagerLoadSettings_end");
}
function XCz(A) {
  if (process.env.CLAUDE_CODE_ENTRYPOINT) return;
  let q = process.argv.slice(2),
    K = q.indexOf("mcp");
  if (K !== -1 && q[K + 1] === "serve") {
    process.env.CLAUDE_CODE_ENTRYPOINT = "mcp";
    return;
  }
  if (isTruthy(process.env.CLAUDE_CODE_ACTION)) {
    process.env.CLAUDE_CODE_ENTRYPOINT = "claude-code-github-action";
    return;
  }
  process.env.CLAUDE_CODE_ENTRYPOINT = A ? "sdk-cli" : "cli";
}
async function cliMain() {
  (Bq("main_function_start"),
    (process.env.NoDefaultCurrentDirectoryInExePath = "1"),
    PTq(),
    process.on("exit", () => {
      fCz();
    }),
    process.on("SIGINT", () => {
      process.exit(0);
    }),
    Bq("main_warning_handler_initialized"));
  let A = process.argv.slice(2),
    q = A.includes("-p") || A.includes("--print"),
    K = A.includes("--init-only"),
    Y = A.some(($) => $.startsWith("--sdk-url")),
    z = q || K || Y || !process.stdout.isTTY;
  if (z) Ba();
  (OI1(!z), XCz(z));
  let _ = (() => {
    if (process.env.GITHUB_ACTIONS === "true") return "github-action";
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "sdk-ts")
      return "sdk-typescript";
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "sdk-py") return "sdk-python";
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "sdk-cli") return "sdk-cli";
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "claude-vscode")
      return "claude-vscode";
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "local-agent")
      return "local-agent";
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "claude-desktop")
      return "claude-desktop";
    let $ =
      process.env.CLAUDE_CODE_SESSION_ACCESS_TOKEN ||
      process.env.CLAUDE_CODE_WEBSOCKET_AUTH_FILE_DESCRIPTOR;
    if (process.env.CLAUDE_CODE_ENTRYPOINT === "remote" || $) return "remote";
    return "cli";
  })();
  if ((HI1(_), process.env.CLAUDE_CODE_ENVIRONMENT_KIND === "bridge"))
    jI1("remote-control");
  (Bq("main_client_type_determined"),
    DCz(),
    Bq("main_before_run"),
    (process.title = "claude"),
    await setupCommander(),
    Bq("main_after_run"));
}
function PCz(A) {
  let q = 0,
    K = Z66(A);
  if (K.stdin) emitEvent("tengu_stdin_interactive", {});
  let Y = new _i8(),
    z = Oi8();
  return (
    lh1(z),
    {
      getFpsMetrics: () => Y.getMetrics(),
      stats: z,
      renderOptions: {
        ...K,
        onFrame: (w) => {
          if (
            (Y.record(w.durationMs),
            z.observe("frame_duration_ms", w.durationMs),
            nX7())
          )
            return;
          for (let _ of w.flickers) {
            if (_.reason === "resize") continue;
            let $ = Date.now();
            if ($ - q < 1000)
              emitEvent("tengu_flicker", {
                desiredHeight: _.desiredHeight,
                actualHeight: _.availableHeight,
                reason: _.reason,
              });
            q = $;
          }
        },
      },
    }
  );
}
async function WCz(A, q) {
  if (!process.stdin.isTTY && !process.argv.includes("mcp")) {
    if (q === "stream-json") return process.stdin;
    process.stdin.setEncoding("utf8");
    let K = "";
    return (
      process.stdin.on("data", (Y) => {
        K += Y;
      }),
      await new Promise((Y) => {
        process.stdin.on("end", Y);
      }),
      [A, K].filter(Boolean).join(`
`)
    );
  }
  return A;
}
async function setupCommander() {
  Bq("run_function_start");
  function A() {
    let _ = ($) =>
      $.long?.replace(/^--/, "") ?? $.short?.replace(/^-/, "") ?? "";
    return Object.assign(
      { sortSubcommands: !0, sortOptions: !0 },
      { compareOptions: ($, O) => _($).localeCompare(_(O)) },
    );
  }
  let q = new uTq().configureHelp(A()).enablePositionalOptions();
  (Bq("run_commander_initialized"),
    q.hook("preAction", async () => {
      (Bq("preAction_start"),
        await AZq(),
        Bq("preAction_after_mdm"),
        await HTq(),
        Bq("preAction_after_init"),
        IO7(),
        OCz(),
        Bq("preAction_after_migrations"),
        Ey4(),
        Hy8(),
        Bq("preAction_after_remote_settings"),
        Bq("preAction_after_settings_sync"));
    }),
    q
      .name("claude")
      .description(
        "Claude Code - starts an interactive session by default, use -p/--print for non-interactive output",
      )
      .argument("[prompt]", "Your prompt", String)
      .helpOption("-h, --help", "Display help for command")
      .option(
        "-d, --debug [filter]",
        'Enable debug mode with optional category filtering (e.g., "api,hooks" or "!1p,!file")',
        (_) => {
          return !0;
        },
      )
      .addOption(
        new n3("-d2e, --debug-to-stderr", "Enable debug mode (to stderr)")
          .argParser(Boolean)
          .hideHelp(),
      )
      .option(
        "--debug-file <path>",
        "Write debug logs to a specific file path (implicitly enables debug mode)",
        () => !0,
      )
      .option(
        "--verbose",
        "Override verbose mode setting from config",
        () => !0,
      )
      .option(
        "-p, --print",
        "Print response and exit (useful for pipes). Note: The workspace trust dialog is skipped when Claude is run with the -p mode. Only use this flag in directories you trust.",
        () => !0,
      )
      .addOption(
        new n3(
          "--init",
          "Run Setup hooks with init trigger, then continue",
        ).hideHelp(),
      )
      .addOption(
        new n3(
          "--init-only",
          "Run Setup and SessionStart:startup hooks, then exit",
        ).hideHelp(),
      )
      .addOption(
        new n3(
          "--maintenance",
          "Run Setup hooks with maintenance trigger, then continue",
        ).hideHelp(),
      )
      .addOption(
        new n3(
          "--output-format <format>",
          'Output format (only works with --print): "text" (default), "json" (single result), or "stream-json" (realtime streaming)',
        ).choices(["text", "json", "stream-json"]),
      )
      .addOption(
        new n3(
          "--json-schema <schema>",
          'JSON Schema for structured output validation. Example: {"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}',
        ).argParser(String),
      )
      .option(
        "--include-partial-messages",
        "Include partial message chunks as they arrive (only works with --print and --output-format=stream-json)",
        () => !0,
      )
      .addOption(
        new n3(
          "--input-format <format>",
          'Input format (only works with --print): "text" (default), or "stream-json" (realtime streaming input)',
        ).choices(["text", "stream-json"]),
      )
      .option(
        "--mcp-debug",
        "[DEPRECATED. Use --debug instead] Enable MCP debug mode (shows MCP server errors)",
        () => !0,
      )
      .option(
        "--dangerously-skip-permissions",
        "Bypass all permission checks. Recommended only for sandboxes with no internet access.",
        () => !0,
      )
      .option(
        "--allow-dangerously-skip-permissions",
        "Enable bypassing all permission checks as an option, without it being enabled by default. Recommended only for sandboxes with no internet access.",
        () => !0,
      )
      .addOption(
        new n3(
          "--thinking <mode>",
          "Thinking mode: enabled (equivalent to adaptive), disabled",
        )
          .choices(["enabled", "adaptive", "disabled"])
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--max-thinking-tokens <tokens>",
          "[DEPRECATED. Use --thinking instead for newer models] Maximum number of thinking tokens (only works with --print)",
        )
          .argParser(Number)
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--max-turns <turns>",
          "Maximum number of agentic turns in non-interactive mode. This will early exit the conversation after the specified number of turns. (only works with --print)",
        )
          .argParser(Number)
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--max-budget-usd <amount>",
          "Maximum dollar amount to spend on API calls (only works with --print)",
        ).argParser((_) => {
          let $ = Number(_);
          if (isNaN($) || $ <= 0)
            throw Error(
              "--max-budget-usd must be a positive number greater than 0",
            );
          return $;
        }),
      )
      .option(
        "--replay-user-messages",
        "Re-emit user messages from stdin back on stdout for acknowledgment (only works with --input-format=stream-json and --output-format=stream-json)",
        () => !0,
      )
      .addOption(
        new n3(
          "--enable-auth-status",
          "Enable auth status messages in SDK mode",
        )
          .default(!1)
          .hideHelp(),
      )
      .option(
        "--allowedTools, --allowed-tools <tools...>",
        'Comma or space-separated list of tool names to allow (e.g. "Bash(git:*) Edit")',
      )
      .option(
        "--tools <tools...>",
        'Specify the list of available tools from the built-in set. Use "" to disable all tools, "default" to use all tools, or specify tool names (e.g. "Bash,Edit,Read").',
      )
      .option(
        "--disallowedTools, --disallowed-tools <tools...>",
        'Comma or space-separated list of tool names to deny (e.g. "Bash(git:*) Edit")',
      )
      .option(
        "--mcp-config <configs...>",
        "Load MCP servers from JSON files or strings (space-separated)",
      )
      .addOption(
        new n3(
          "--permission-prompt-tool <tool>",
          "MCP tool to use for permission prompts (only works with --print)",
        )
          .argParser(String)
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--system-prompt <prompt>",
          "System prompt to use for the session",
        ).argParser(String),
      )
      .addOption(
        new n3("--system-prompt-file <file>", "Read system prompt from a file")
          .argParser(String)
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--append-system-prompt <prompt>",
          "Append a system prompt to the default system prompt",
        ).argParser(String),
      )
      .addOption(
        new n3(
          "--append-system-prompt-file <file>",
          "Read system prompt from a file and append to the default system prompt",
        )
          .argParser(String)
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--permission-mode <mode>",
          "Permission mode to use for the session",
        )
          .argParser(String)
          .choices(Wy),
      )
      .option(
        "-c, --continue",
        "Continue the most recent conversation in the current directory",
        () => !0,
      )
      .option(
        "-r, --resume [value]",
        "Resume a conversation by session ID, or open interactive picker with optional search term",
        (_) => _ || !0,
      )
      .option(
        "--fork-session",
        "When resuming, create a new session ID instead of reusing the original (use with --resume or --continue)",
        () => !0,
      )
      .option(
        "--from-pr [value]",
        "Resume a session linked to a PR by PR number/URL, or open interactive picker with optional search term",
        (_) => _ || !0,
      )
      .option(
        "--no-session-persistence",
        "Disable session persistence - sessions will not be saved to disk and cannot be resumed (only works with --print)",
      )
      .addOption(
        new n3(
          "--resume-session-at <message id>",
          "When resuming, only messages up to and including the assistant message with <message.id> (use with --resume in print mode)",
        )
          .argParser(String)
          .hideHelp(),
      )
      .addOption(
        new n3(
          "--rewind-files <user-message-id>",
          "Restore files to state at the specified user message and exit (requires --resume)",
        ).hideHelp(),
      )
      .option(
        "--model <model>",
        "Model for the current session. Provide an alias for the latest model (e.g. 'sonnet' or 'opus') or a model's full name (e.g. 'claude-sonnet-4-6').",
      )
      .addOption(
        new n3(
          "--effort <level>",
          "Effort level for the current session (low, medium, high)",
        ).argParser((_) => {
          let $ = ["low", "medium", "high", "max"];
          if (!$.includes(_))
            throw new bTq(`It must be one of: ${$.join(", ")}`);
          return _;
        }),
      )
      .option(
        "--agent <agent>",
        "Agent for the current session. Overrides the 'agent' setting.",
      )
      .option(
        "--betas <betas...>",
        "Beta headers to include in API requests (API key users only)",
      )
      .option(
        "--fallback-model <model>",
        "Enable automatic fallback to specified model when default model is overloaded (only works with --print)",
      )
      .option(
        "--settings <file-or-json>",
        "Path to a settings JSON file or a JSON string to load additional settings from",
      )
      .option(
        "--add-dir <directories...>",
        "Additional directories to allow tool access to",
      )
      .option(
        "--ide",
        "Automatically connect to IDE on startup if exactly one valid IDE is available",
        () => !0,
      )
      .option(
        "--strict-mcp-config",
        "Only use MCP servers from --mcp-config, ignoring all other MCP configurations",
        () => !0,
      )
      .option(
        "--session-id <uuid>",
        "Use a specific session ID for the conversation (must be a valid UUID)",
      )
      .option(
        "--agents <json>",
        `JSON object defining custom agents (e.g. '{"reviewer": {"description": "Reviews code", "prompt": "You are a code reviewer"}}')`,
      )
      .option(
        "--setting-sources <sources>",
        "Comma-separated list of setting sources to load (user, project, local).",
      )
      .option(
        "--plugin-dir <paths...>",
        "Load plugins from directories for this session only (repeatable)",
      )
      .option("--disable-slash-commands", "Disable all skills", () => !0)
      .option("--chrome", "Enable Claude in Chrome integration")
      .option("--no-chrome", "Disable Claude in Chrome integration")
      .option(
        "--file <specs...>",
        "File resources to download at startup. Format: file_id:relative_path (e.g., --file file_abc:doc.txt file_def:img.png)",
      )
      .action(async (_, $) => {
        if ((Bq("action_handler_start"), _ === "code"))
          (emitEvent("tengu_code_prompt_ignored", {}),
            console.warn(
              H1.yellow("Tip: You can launch Claude Code with just `claude`"),
            ),
            (_ = void 0));
        if (_ && typeof _ === "string" && !/\s/.test(_) && _.length > 0)
          emitEvent("tengu_single_word_prompt", { length: _.length });
        let {
            debug: O = !1,
            debugToStderr: H = !1,
            dangerouslySkipPermissions: j,
            allowDangerouslySkipPermissions: J = !1,
            tools: D = [],
            allowedTools: X = [],
            disallowedTools: M = [],
            mcpConfig: P = [],
            permissionMode: W,
            addDir: G = [],
            fallbackModel: Z,
            betas: f = [],
            ide: N = !1,
            sessionId: V,
            includePartialMessages: v,
            pluginDir: L = [],
          } = $,
          S,
          I = $.agents,
          B = $.agent;
        if (L.length > 0) (TI1(L), LG());
        let { outputFormat: h, inputFormat: F } = $,
          g = $.verbose ?? getSettings().verbose,
          u = $.print,
          U = $.init ?? !1,
          c = $.initOnly ?? !1,
          d = $.maintenance ?? !1,
          a = $.disableSlashCommands || !1,
          e = !1,
          j6 = e ? (typeof e === "string" ? e : CG8) : void 0,
          P6 = gT6() ? $.worktree : void 0,
          f6 = typeof P6 === "string" ? P6 : void 0,
          q6 = P6 !== void 0,
          A6;
        if (f6) {
          let F8 = f6.match(
              /^https?:\/\/github\.com\/[^/]+\/[^/]+\/pull\/(\d+)\/?(?:[?#].*)?$/i,
            ),
            O7 = f6.match(/^#(\d+)$/),
            U6 = F8?.[1] ?? O7?.[1];
          if (U6) ((A6 = parseInt(U6, 10)), (f6 = void 0));
        }
        let D6 = gT6() && $.tmux === !0;
        if (D6) {
          if (!q6)
            (process.stderr.write(
              H1.red(`Error: --tmux requires --worktree
`),
            ),
              process.exit(1));
          if (i8() === "windows")
            (process.stderr.write(
              H1.red(`Error: --tmux is not supported on Windows
`),
            ),
              process.exit(1));
          if (!(await Wb8()))
            (process.stderr.write(
              H1.red(`Error: tmux is not installed.
${Gb8()}
`),
            ),
              process.exit(1));
        }
        let G6;
        if (D7()) {
          let F8 = TCz($);
          G6 = F8;
          let O7 = F8.agentId || F8.agentName || F8.teamName,
            U6 = F8.agentId && F8.agentName && F8.teamName;
          if (O7 && !U6)
            (process.stderr.write(
              H1.red(`Error: --agent-id, --agent-name, and --team-name must all be provided together
`),
            ),
              process.exit(1));
          if (F8.agentId && F8.agentName && F8.teamName)
            Lbq().setDynamicTeamContext?.({
              agentId: F8.agentId,
              agentName: F8.agentName,
              teamName: F8.teamName,
              color: F8.agentColor,
              planModeRequired: F8.planModeRequired ?? !1,
              parentSessionId: F8.parentSessionId,
            });
          if (F8.teammateMode)
            eRz().setCliTeammateModeOverride?.(F8.teammateMode);
        }
        let v6 = $.sdkUrl ?? void 0,
          T6 = v || isTruthy(process.env.CLAUDE_CODE_INCLUDE_PARTIAL_MESSAGES);
        if (v6) {
          if (!F) F = "stream-json";
          if (!h) h = "stream-json";
          if ($.verbose === void 0) g = !0;
          if (!$.print) u = !0;
        }
        let z6 = $.teleport ?? null,
          H6 = $.remote,
          _6 = H6 === !0 ? "" : (H6 ?? null);
        if (V) {
          if (($.continue || $.resume) && !$.forkSession)
            (process.stderr.write(
              H1.red(`Error: --session-id can only be used with --continue or --resume if --fork-session is also specified.
`),
            ),
              process.exit(1));
          if (!v6) {
            let F8 = Tk(V);
            if (!F8)
              (process.stderr.write(
                H1.red(`Error: Invalid session ID. Must be a valid UUID.
`),
              ),
                process.exit(1));
            if (Xn6(F8))
              (process.stderr.write(
                H1.red(`Error: Session ID ${F8} is already in use.
`),
              ),
                process.exit(1));
          }
        }
        let K6 = $.file;
        if (K6 && K6.length > 0) {
          let F8 = _G();
          if (!F8)
            (process.stderr.write(
              H1.red(`Error: Session token required for file downloads. CLAUDE_CODE_SESSION_ACCESS_TOKEN must be set.
`),
            ),
              process.exit(1));
          let O7 = process.env.CLAUDE_CODE_REMOTE_SESSION_ID || getSessionId(),
            U6 = FTq(K6);
          if (U6.length > 0) {
            let r6 = {
              baseUrl: process.env.ANTHROPIC_BASE_URL || r7().BASE_API_URL,
              oauthToken: F8,
              sessionId: O7,
            };
            S = gTq(U6, r6);
          }
        }
        let s = C7();
        if (Z && $.model && Z === $.model)
          (process.stderr.write(
            H1.red(`Error: Fallback model cannot be the same as the main model. Please specify a different model for --fallback-model.
`),
          ),
            process.exit(1));
        if ($.effort === "max" && (!s || Y7())) {
          let F8 = !s
            ? 'Effort level "max" is not available in interactive mode.'
            : 'Effort level "max" is not available for Claude.ai subscribers.';
          (process.stderr.write(
            H1.red(`Error: ${F8} Please use "low", "medium", or "high".
`),
          ),
            process.exit(1));
        }
        let t = $.systemPrompt;
        if ($.systemPromptFile) {
          if ($.systemPrompt)
            (process.stderr.write(
              H1.red(`Error: Cannot use both --system-prompt and --system-prompt-file. Please use only one.
`),
            ),
              process.exit(1));
          try {
            let F8 = DS1($.systemPromptFile);
            t = Ebq(F8, "utf8");
          } catch (F8) {
            if (F8.code === "ENOENT")
              (process.stderr.write(
                H1.red(`Error: System prompt file not found: ${DS1($.systemPromptFile)}
`),
              ),
                process.exit(1));
            (process.stderr.write(
              H1.red(`Error reading system prompt file: ${F8 instanceof Error ? F8.message : String(F8)}
`),
            ),
              process.exit(1));
          }
        }
        let O6 = $.appendSystemPrompt;
        if ($.appendSystemPromptFile) {
          if ($.appendSystemPrompt)
            (process.stderr.write(
              H1.red(`Error: Cannot use both --append-system-prompt and --append-system-prompt-file. Please use only one.
`),
            ),
              process.exit(1));
          try {
            let F8 = DS1($.appendSystemPromptFile);
            if (!ybq(F8))
              (process.stderr.write(
                H1.red(`Error: Append system prompt file not found: ${F8}
`),
              ),
                process.exit(1));
            O6 = Ebq(F8, "utf8");
          } catch (F8) {
            (process.stderr.write(
              H1.red(`Error reading append system prompt file: ${F8 instanceof Error ? F8.message : String(F8)}
`),
            ),
              process.exit(1));
          }
        }
        if (D7() && G6?.agentId && G6?.agentName && G6?.teamName) {
          let F8 = tRz().TEAMMATE_SYSTEM_PROMPT_ADDENDUM;
          O6 = O6
            ? `${O6}

${F8}`
            : F8;
        }
        let { mode: X6, notification: E6 } = SPq({
          permissionModeCli: W,
          dangerouslySkipPermissions: j,
        });
        VI1(X6 === "bypassPermissions");
        let L6 = {};
        if (P && P.length > 0) {
          let F8 = P.map((r6) => r6.trim()).filter((r6) => r6.length > 0),
            O7 = {},
            U6 = [];
          for (let r6 of F8) {
            let N1 = null,
              L1 = [],
              U1 = s3(r6);
            if (U1) {
              let E8 = Jp6({
                configObject: U1,
                filePath: "command line",
                expandVars: !0,
                scope: "dynamic",
              });
              if (E8.config) N1 = E8.config.mcpServers;
              else L1 = E8.errors;
            } else {
              let E8 = DS1(r6),
                H8 = FW6({ filePath: E8, expandVars: !0, scope: "dynamic" });
              if (H8.config) N1 = H8.config.mcpServers;
              else L1 = H8.errors;
            }
            if (L1.length > 0) U6.push(...L1);
            else if (N1) O7 = { ...O7, ...N1 };
          }
          if (U6.length > 0) {
            let r6 = U6.map(
              (N1) => `${N1.path ? N1.path + ": " : ""}${N1.message}`,
            ).join(`
`);
            throw Error(`Invalid MCP configuration:
${r6}`);
          }
          if (Object.keys(O7).length > 0) {
            if (Object.keys(O7).some(D96))
              throw Error(
                `Invalid MCP configuration: "${uR}" is a reserved MCP name.`,
              );
            let r6 = n76(O7, (N1) => ({ ...N1, scope: "dynamic" }));
            L6 = { ...L6, ...r6 };
          }
        }
        let h6 = $;
        NI1(h6.chrome);
        let g6 = GL1(h6.chrome) && Y7(),
          y6 = !g6 && zV6();
        if (g6) {
          let F8 = i8();
          try {
            emitEvent("tengu_claude_in_chrome_setup", { platform: F8 });
            let { mcpConfig: O7, allowedTools: U6, systemPrompt: r6 } = LU8();
            if (((L6 = { ...L6, ...O7 }), X.push(...U6), r6))
              O6 = O6
                ? `${r6}

${O6}`
                : r6;
          } catch (O7) {
            (emitEvent("tengu_claude_in_chrome_setup_failed", { platform: F8 }),
              writeDebugLog(`[Claude in Chrome] Error: ${O7}`),
              sendError(O7 instanceof Error ? O7 : Error(String(O7))),
              console.error("Error: Failed to run with Claude in Chrome."),
              process.exit(1));
          }
        } else if (y6)
          try {
            let { mcpConfig: F8 } = LU8();
            ((L6 = { ...L6, ...F8 }),
              (O6 = O6
                ? `${O6}

${kU8}`
                : kU8));
          } catch (F8) {
            writeDebugLog(`[Claude in Chrome] Error (auto-enable): ${F8}`);
          }
        let r = $.strictMcpConfig || !1;
        if (Dp6()) {
          if (r)
            (process.stderr.write(
              H1.red(
                "You cannot use --strict-mcp-config when an enterprise MCP config is present",
              ),
            ),
              process.exit(1));
          if (L6 && !A$4(L6))
            (process.stderr.write(
              H1.red(
                "You cannot dynamically configure MCP servers when an enterprise MCP config is present",
              ),
            ),
              process.exit(1));
        }
        pk6(G);
        let Z6 = await hPq({
            allowedToolsCli: X,
            disallowedToolsCli: M,
            baseToolsCli: D,
            permissionMode: X6,
            allowDangerouslySkipPermissions: J,
            addDirs: G,
          }),
          S6 = Z6.toolPermissionContext,
          {
            warnings: C6,
            dangerousPermissions: d6,
            overlyBroadBashPermissions: o6,
          } = Z6;
        (C6.forEach((F8) => {
          console.error(F8);
        }),
          Pi4(),
          writeDebugLog("[STARTUP] Loading MCP configs..."));
        let K1 = Date.now(),
          x6 = r ? Promise.resolve({ servers: {} }) : s ? Pg() : pW6();
        if (F && F !== "text" && F !== "stream-json")
          (console.error(`Error: Invalid input format "${F}".`),
            process.exit(1));
        if (F === "stream-json" && h !== "stream-json")
          (console.error(
            "Error: --input-format=stream-json requires output-format=stream-json.",
          ),
            process.exit(1));
        if (v6) {
          if (F !== "stream-json" || h !== "stream-json")
            (console.error(
              "Error: --sdk-url requires both --input-format=stream-json and --output-format=stream-json.",
            ),
              process.exit(1));
        }
        let t6 = !!$.replayUserMessages;
        if ($.replayUserMessages) {
          if (F !== "stream-json" || h !== "stream-json")
            (console.error(
              "Error: --replay-user-messages requires both --input-format=stream-json and --output-format=stream-json.",
            ),
              process.exit(1));
        }
        if (T6) {
          if (!s || h !== "stream-json")
            (dn(
              "Error: --include-partial-messages requires --print and --output-format=stream-json.",
            ),
              process.exit(1));
        }
        if ($.sessionPersistence === !1 && !s)
          (dn(
            "Error: --no-session-persistence can only be used with --print mode.",
          ),
            process.exit(1));
        let j1 = await WCz(_ || "", F ?? "text");
        Bq("action_after_input_prompt");
        let R1 = A0(S6);
        if ((Bq("action_tools_loaded"), !s))
          Promise.resolve()
            .then(() => (ZI6(), Dj7))
            .then((F8) => F8.initLayout());
        let M1;
        if (nX4({ isNonInteractiveSession: s }) && $.jsonSchema)
          M1 = w8($.jsonSchema);
        if (M1) {
          let F8 = KW1(M1);
          if (F8)
            ((R1 = [...R1, F8]),
              emitEvent("tengu_structured_output_enabled", {
                schema_property_count: Object.keys(M1.properties || {}).length,
                has_required_fields: Boolean(M1.required),
              }));
          else
            emitEvent("tengu_structured_output_failure", {
              error: "Invalid JSON schema",
            });
        }
        (Bq("action_before_setup"), writeDebugLog("[STARTUP] Running setup()..."));
        let M6 = Date.now(),
          { setup: V6 } = await Promise.resolve().then(() => (mR1(), uR1)),
          s6 = void 0;
        (await V6(qCz(), X6, J, q6, f6, D6, V ? Tk(V) : void 0, A6, s6),
          writeDebugLog(`[STARTUP] setup() completed in ${Date.now() - M6}ms`),
          Bq("action_after_setup"));
        let O1 = $.model === "default" ? KW() : $.model,
          w1 = Z === "default" ? KW() : Z,
          J1 = y1();
        writeDebugLog("[STARTUP] Loading commands and agents...");
        let g1 = Date.now(),
          [Z1, I1] = await Promise.all([rG(J1), Eg(J1)]);
        (writeDebugLog(`[STARTUP] Commands and agents loaded in ${Date.now() - g1}ms`),
          Bq("action_commands_loaded"));
        let A8 = [];
        if (I)
          try {
            let F8 = s3(I);
            if (F8) A8 = _P1(F8, "flagSettings");
          } catch (F8) {
            sendError(F8 instanceof Error ? F8 : Error(String(F8)));
          }
        let AA = [...I1.allAgents, ...A8],
          qA = { ...I1, allAgents: AA, activeAgents: tk(AA) },
          y7 = B ?? U7().agent,
          BA;
        if (y7) {
          if (((BA = qA.activeAgents.find((F8) => F8.agentType === y7)), !BA))
            writeDebugLog(
              `Warning: agent "${y7}" not found. Available agents: ${qA.activeAgents.map((F8) => F8.agentType).join(", ")}. Using default behavior.`,
            );
        }
        if ((tp(BA?.agentType), BA))
          emitEvent("tengu_agent_flag", {
            agentType: LD(BA) ? BA.agentType : "custom",
            ...(B && { source: "cli" }),
          });
        if (BA?.agentType) Pn6(getSessionId(), BA.agentType);
        if (s && BA && !t && !LD(BA)) {
          let F8 = BA.getSystemPrompt();
          if (F8) t = F8;
        }
        let gA = O1;
        if (!gA && BA?.model && BA.model !== "inherit") gA = O5(BA.model);
        (LW(gA), eh1(eR() || null));
        let GA = Q_6(),
          fK = O5(GA ?? KW());
        if (
          D7() &&
          G6?.agentId &&
          G6?.agentName &&
          G6?.teamName &&
          G6?.agentType
        ) {
          let F8 = qA.activeAgents.find((O7) => O7.agentType === G6.agentType);
          if (F8) {
            let O7;
            if (F8.source === "built-in")
              writeDebugLog(
                `[teammate] Built-in agent ${G6.agentType} - skipping custom prompt (not supported)`,
              );
            else O7 = F8.getSystemPrompt();
            if (F8.memory)
              emitEvent("tengu_agent_memory_loaded", {
                ...{},
                scope: F8.memory,
                source: "teammate",
              });
            if (O7) {
              let U6 = `
# Custom Agent Instructions
${O7}`;
              O6 = O6
                ? `${O6}

${U6}`
                : U6;
            }
          } else
            writeDebugLog(
              `[teammate] Custom agent ${G6.agentType} not found in available agents`,
            );
        }
        XS1($);
        let v4, a4, UA;
        if (!s) {
          let F8 = PCz(!1);
          ((a4 = F8.getFpsMetrics), (UA = F8.stats));
          let { createRoot: O7 } = await Promise.resolve().then(
            () => (Q6(), oI6),
          );
          ((v4 = await O7(F8.renderOptions)),
            writeDebugLog("[STARTUP] Running showSetupScreens()..."));
          let U6 = Date.now(),
            r6 = await showSetupScreens(v4, X6, J, Z1, g6);
          if (
            (writeDebugLog(
              `[STARTUP] showSetupScreens() completed in ${Date.now() - U6}ms`,
            ),
            r6 && _?.trim().toLowerCase() === "/login")
          )
            _ = "";
          if (r6) (sG1(), kU6(), Fv.cache?.clear?.(), Lf6());
        }
        if (process.exitCode !== void 0) {
          writeDebugLog("Graceful shutdown initiated, skipping further initialization");
          return;
        }
        if ((Fe4(), !s)) {
          let { errors: F8 } = qz6(),
            O7 = F8.filter((U6) => !U6.mcpErrorMetadata);
          if (O7.length > 0) {
            let { InvalidSettingsDialog: U6 } = await Promise.resolve().then(
              () => (tvq(), svq),
            );
            await jp(v4, (r6) =>
              r$.default.createElement(U6, {
                settingsErrors: O7,
                onContinue: r6,
                onExit: () => _3(1),
              }),
            );
          }
        }
        if ((Y14().catch((F8) => sendError(F8)), xOq(), AL1(), !s)) HNq();
        let { servers: X4 } = await x6;
        writeDebugLog(`[STARTUP] MCP configs loaded in ${Date.now() - K1}ms`);
        let H3 = { ...X4, ...L6 },
          Zz = {},
          UK = {};
        for (let [F8, O7] of Object.entries(H3)) {
          let U6 = O7;
          if (U6.type === "sdk") Zz[F8] = U6;
          else UK[F8] = U6;
        }
        Bq("action_mcp_configs_loaded");
        let gz = sM1(UK),
          fz =
            c || U || d || s
              ? null
              : xP("startup", { agentType: BA?.agentType, model: fK }),
          W9 = (j1 || s) && !isTruthy(process.env.MCP_CONNECTION_NONBLOCKING),
          K2 = W9 ? void 0 : gz,
          Tz,
          d5;
        if (W9 && fz) [Tz, d5] = await Promise.all([gz, fz]);
        else if (W9) ((Tz = await gz), (d5 = []));
        else ((Tz = { clients: [], tools: [], commands: [] }), (d5 = []));
        let { clients: Hw, tools: I9, commands: Y2 } = Tz,
          Jq = cT6(),
          c5 = Jq !== !1 ? { type: "adaptive" } : { type: "disabled" };
        if ($.thinking === "adaptive" || $.thinking === "enabled")
          ((Jq = !0), (c5 = { type: "adaptive" }));
        else if ($.thinking === "disabled")
          ((Jq = !1), (c5 = { type: "disabled" }));
        else {
          let F8 = process.env.MAX_THINKING_TOKENS
            ? parseInt(process.env.MAX_THINKING_TOKENS, 10)
            : $.maxThinkingTokens;
          if (F8 !== void 0) {
            if (F8 > 0)
              ((Jq = !0), (c5 = { type: "enabled", budgetTokens: F8 }));
            else if (F8 === 0) ((Jq = !1), (c5 = { type: "disabled" }));
          }
        }
        if (
          ($8("info", "started", {
            version: {
              ISSUES_EXPLAINER:
                "report the issue at https://github.com/anthropics/claude-code/issues",
              PACKAGE_URL: "klaudia",
              README_URL: "https://code.claude.com/docs/en/overview",
              VERSION: "2.1.66-klaudia",
              FEEDBACK_CHANNEL:
                "https://github.com/anthropics/claude-code/issues",
              BUILD_TIME: "2026-03-04T00:18:36Z",
            }.VERSION,
            is_native_binary: T9(),
          }),
          Xq(async () => {
            $8("info", "exited");
          }),
          ZCz({
            hasInitialPrompt: Boolean(_),
            hasStdin: Boolean(j1),
            verbose: g,
            debug: O,
            debugToStderr: H,
            print: u ?? !1,
            outputFormat: h ?? "text",
            inputFormat: F ?? "text",
            numAllowedTools: X.length,
            numDisallowedTools: M.length,
            mcpClientCount: Object.keys(H3).length,
            worktreeEnabled: q6,
            skipWebFetchPreflight: U7().skipWebFetchPreflight,
            githubActionInputs: process.env.GITHUB_ACTION_INPUTS,
            dangerouslySkipPermissionsPassed: j ?? !1,
            permissionMode: X6,
            modeIsBypass: X6 === "bypassPermissions",
            allowDangerouslySkipPermissionsPassed: J,
            systemPromptFlag: t
              ? $.systemPromptFile
                ? "file"
                : "flag"
              : void 0,
            appendSystemPromptFlag: O6
              ? $.appendSystemPromptFile
                ? "file"
                : "flag"
              : void 0,
            thinkingConfig: c5,
          }),
          KWq(UK, S6),
          NP1(null, "initialization"),
          loadManagedSettings(),
          s)
        )
          (await oG8(), Bq("action_after_plugins_init"), Lv8());
        else
          oG8().then(() => {
            (Bq("action_after_plugins_init"), Lv8());
          });
        let KY = c || U ? "init" : d ? "maintenance" : null;
        if (c) {
          (S86(),
            await EP1("init", { forceSyncExecution: !0 }),
            await xP("startup", { forceSyncExecution: !0 }),
            _3(0));
          return;
        }
        if (s) {
          if (h === "stream-json" || h === "json") je8(!0);
          (S86(), pl8());
          let F8 = a
              ? []
              : Z1.filter(
                  (L1) =>
                    (L1.type === "prompt" && !L1.disableNonInteractive) ||
                    (L1.type === "local" && L1.supportsNonInteractive),
                ),
            O7 = WV6(),
            U6 = {
              ...O7,
              mcp: { ...O7.mcp, clients: Hw, commands: Y2, tools: I9 },
              toolPermissionContext: S6,
              effortValue: VK6($.effort) ?? Kb6(),
              ...(xq() ? { fastMode: Qc8(gA ?? null) } : {}),
            };
          if (xq() && U7().fastMode === !0 && !U6.fastMode) {
            let L1 = q86();
            if (L1)
              process.stderr.write(`[WARN] ${L1}. Using ${LE}.
`);
          }
          let r6 = Xy1(U6, h86);
          if (S6.mode === "bypassPermissions" || J) IPq(S6);
          if ($.sessionPersistence === !1) vI1(!0);
          (AI1(RbA(f)),
            Fr8(),
            Promise.resolve()
              .then(() => (xi8(), Mkq))
              .then((L1) => L1.startBackgroundHousekeeping()));
          let { runHeadless: N1 } = await Promise.resolve().then(
            () => (initHeadless(), headlessExports),
          );
          N1(
            j1,
            async () => r6.getState(),
            r6.setState,
            F8,
            R1,
            Zz,
            qA.activeAgents,
            {
              continue: $.continue,
              resume: $.resume,
              verbose: g,
              outputFormat: h,
              jsonSchema: M1,
              permissionPromptToolName: $.permissionPromptTool,
              allowedTools: X,
              thinkingConfig: c5,
              maxTurns: $.maxTurns,
              maxBudgetUsd: $.maxBudgetUsd,
              systemPrompt: t,
              appendSystemPrompt: O6,
              userSpecifiedModel: O1,
              fallbackModel: w1,
              teleport: z6,
              sdkUrl: v6,
              replayUserMessages: t6,
              includePartialMessages: T6,
              forkSession: $.forkSession || !1,
              resumeSessionAt: $.resumeSessionAt || void 0,
              rewindFiles: $.rewindFiles,
              enableAuthStatus: $.enableAuthStatus,
              agent: B,
              setupTrigger: KY ?? void 0,
              mcpDeferredPromise: K2,
            },
          );
          return;
        }
        let { App: SY } = await Promise.resolve().then(() => (initApp(), appExports));
        emitEvent("tengu_startup_manual_model_config", {
          cli_flag: $.model,
          env_var: process.env.ANTHROPIC_MODEL,
          settings_file: (U7() || {}).model,
          subscriptionType: kK(),
          agent: y7,
        });
        let c4 = NR1(fK),
          l5 = [];
        if (E6)
          l5.push({
            key: "permission-mode-notification",
            text: E6,
            priority: "high",
          });
        if (c4)
          l5.push({
            key: "model-deprecation-warning",
            text: c4,
            color: "warning",
            priority: "high",
          });
        if (o6.length > 0) {
          let F8 = [...new Set(o6.map((O7) => O7.sourceDisplay))].join(", ");
          l5.push({
            key: "overly-broad-bash-notification",
            text: `Bash(*) allow rule from ${F8} was ignored — Bash(*) is not available for Ants, please use auto-mode instead`,
            color: "warning",
            priority: "high",
          });
        }
        let aY = {
            ...S6,
            mode: D7() && Lbq().isPlanModeRequired() ? "plan" : S6.mode,
          },
          R5 = {
            settings: U7(),
            tasks: {},
            verbose: g ?? getSettings().verbose ?? !1,
            mainLoopModel: GA,
            mainLoopModelForSession: null,
            isBriefOnly: !1,
            expandedView: getSettings().showSpinnerTree
              ? "teammates"
              : getSettings().showExpandedTodos
                ? "tasks"
                : "none",
            showTeammateMessagePreview: D7() ? !1 : void 0,
            selectedIPAgentIndex: -1,
            viewSelectionMode: "none",
            toolPermissionContext: aY,
            agent: BA?.agentType,
            agentDefinitions: qA,
            mcp: { clients: [], tools: [], commands: [], resources: {} },
            plugins: {
              enabled: [],
              disabled: [],
              commands: [],
              agents: [],
              errors: [],
              installationStatus: { marketplaces: [], plugins: [] },
              needsRefresh: !1,
            },
            statusLineText: void 0,
            remoteSessionUrl: void 0,
            replBridgeEnabled: L16(),
            replBridgeExplicit: !1,
            replBridgeConnected: !1,
            replBridgeSessionActive: !1,
            replBridgeReconnecting: !1,
            replBridgeConnectUrl: void 0,
            replBridgeSessionUrl: void 0,
            replBridgeEnvironmentId: void 0,
            replBridgeSessionId: void 0,
            replBridgeError: void 0,
            showRemoteCallout: !1,
            notifications: { current: null, queue: l5 },
            elicitation: { queue: [] },
            todos: {},
            fileHistory: {
              snapshots: [],
              trackedFiles: new Set(),
              snapshotSequence: 0,
            },
            attribution: eT6(),
            thinkingEnabled: Jq,
            promptSuggestionEnabled: LN1(),
            feedbackSurvey: {
              timeLastShown: null,
              submitCountAtLastAppearance: null,
            },
            sessionHooks: {},
            inbox: { messages: [] },
            promptSuggestion: {
              text: null,
              promptId: null,
              shownAt: 0,
              acceptedAt: 0,
              generationRequestId: null,
            },
            speculation: Tz6,
            speculationSessionTimeSavedMs: 0,
            skillImprovement: { suggestion: null },
            workerSandboxPermissions: { queue: [], selectedIndex: 0 },
            pendingWorkerRequest: null,
            pendingSandboxRequest: null,
            prStatus: {
              number: null,
              url: null,
              reviewState: null,
              lastUpdated: 0,
            },
            authVersion: 0,
            initialMessage: j1
              ? { message: K8({ content: String(j1) }) }
              : null,
            effortValue: VK6($.effort) ?? Kb6(),
            activeOverlays: new Set(),
            fastMode: Qc8(fK),
            teamContext: lTq?.(),
          };
        if (j1) IK6(String(j1));
        let G9 = I9;
        wCz();
        let Z_ = null,
          { REPL: _q } = await Promise.resolve().then(() => (xr8(), Qxq)),
          z2 = Z_
            ? Z_.then((F8) => F8.createSessionTurnUploader()).catch(() => null)
            : null,
          sY = {
            debug: O || H,
            commands: [...Z1, ...Y2],
            initialTools: G9,
            mcpClients: Hw,
            autoConnectIdeFlag: N,
            mainThreadAgentDefinition: BA,
            disableSlashCommands: a,
            dynamicMcpConfig: L6,
            strictMcpConfig: r,
            systemPrompt: t,
            appendSystemPrompt: O6,
            taskListId: j6,
            thinkingConfig: c5,
            ...(z2
              ? {
                  onTurnComplete: (F8) => {
                    z2.then((O7) => O7?.(F8));
                  },
                }
              : {}),
          },
          g3 = {
            modeApi: ACz,
            mainThreadAgentDefinition: BA,
            agentDefinitions: qA,
            currentCwd: J1,
            cliAgents: A8,
            initialState: R5,
          };
        if ($.continue) {
          let F8 = !1;
          try {
            let O7 = performance.now(),
              { clearSessionCaches: U6 } = await Promise.resolve().then(
                () => (Pk1(), Ig8),
              );
            U6();
            let r6 = await j16(void 0, void 0);
            if (!r6)
              return (
                emitEvent("tengu_continue", { success: !1 }),
                await c86(v4, "No conversation found to continue")
              );
            let N1 = await Pi8(
              r6,
              { forkSession: !!$.forkSession, includeAttribution: !0 },
              g3,
            );
            if (N1.restoredAgentDef) BA = N1.restoredAgentDef;
            if (Zu8(N1.messages)) W16();
            (gr8($),
              XS1($),
              emitEvent("tengu_continue", {
                success: !0,
                resume_duration_ms: Math.round(performance.now() - O7),
              }),
              (F8 = !0),
              await To6(
                v4,
                r$.default.createElement(
                  SY,
                  {
                    getFpsMetrics: a4,
                    stats: UA,
                    initialState: N1.initialState,
                  },
                  r$.default.createElement(_q, {
                    ...sY,
                    mainThreadAgentDefinition: N1.restoredAgentDef ?? BA,
                    initialMessages: N1.messages,
                    initialFileHistorySnapshots: N1.fileHistorySnapshots,
                    initialAgentName: N1.agentName,
                    initialAgentColor: N1.agentColor,
                  }),
                ),
              ));
          } catch (O7) {
            if (!F8) emitEvent("tengu_continue", { success: !1 });
            (sendError(O7 instanceof Error ? O7 : Error(String(O7))), process.exit(1));
          }
        } else if ($.resume || $.fromPr || z6 || _6 !== null) {
          let { clearSessionCaches: F8 } = await Promise.resolve().then(
            () => (Pk1(), Ig8),
          );
          F8();
          let O7 = null,
            U6 = void 0,
            r6 = Tk($.resume),
            N1 = void 0,
            L1 = null,
            U1 = void 0;
          if ($.fromPr) {
            if ($.fromPr === !0) U1 = !0;
            else if (typeof $.fromPr === "string") U1 = $.fromPr;
          }
          if ($.resume && typeof $.resume === "string" && !r6) {
            let H8 = $.resume.trim();
            if (H8) {
              let V8 = await xF(H8, { exact: !0 });
              if (V8.length === 1) ((L1 = V8[0]), (r6 = bw(L1) ?? null));
              else N1 = H8;
            }
          }
          if (_6 !== null || z6) {
            if ((await vU6(), !ZH("allow_remote_sessions")))
              return await c86(
                v4,
                "Error: Remote sessions are disabled by your organization's policy.",
                () => rq(1),
              );
          }
          if (_6 !== null) {
            let H8 = _6.length > 0,
              V8 = jA("tengu_remote_backend", !1);
            if (!V8 && !H8)
              return await c86(
                v4,
                `Error: --remote requires a description.
Usage: claude --remote "your task description"`,
                () => rq(1),
              );
            emitEvent("tengu_remote_create_session", {
              has_initial_prompt: String(H8),
            });
            let JA = await Qj(),
              r8 = await Gs4(
                v4,
                H8 ? _6 : null,
                new AbortController().signal,
                JA || void 0,
              );
            if (!r8)
              return (
                emitEvent("tengu_remote_create_session_error", {
                  error: "unable_to_create_session",
                }),
                await c86(v4, "Error: Unable to create remote session", () =>
                  rq(1),
                )
              );
            if (
              (emitEvent("tengu_remote_create_session_success", { session_id: r8.id }),
              !V8)
            )
              (process.stdout.write(`Created remote session: ${r8.title}
`),
                process.stdout.write(`View: https://claude.ai/code/${r8.id}?m=0
`),
                process.stdout.write(`Resume with: claude --teleport ${r8.id}
`),
                await rq(0),
                process.exit(0));
            (xI1(!0), Z0(XX(r8.id)));
            let CA;
            try {
              CA = await xN();
            } catch (zY) {
              return (
                sendError(
                  zY instanceof Error
                    ? zY
                    : Error("Failed to authenticate for remote session"),
                ),
                await c86(
                  v4,
                  `Error: ${zY instanceof Error ? zY.message : "Failed to authenticate"}`,
                  () => rq(1),
                )
              );
            }
            let R7 = xNq(r8.id, CA.accessToken, CA.orgUUID, H8),
              i4 = `https://claude.ai/code/${r8.id}?m=0`,
              y3 = IX(
                `/remote-control is active. Code in CLI or at ${i4}`,
                "info",
              ),
              Dq = H8 ? K8({ content: _6 }) : null,
              P5 = { ...R5, remoteSessionUrl: i4 },
              YY = DPq(Z1);
            await To6(
              v4,
              r$.default.createElement(
                SY,
                { getFpsMetrics: a4, stats: UA, initialState: P5 },
                r$.default.createElement(_q, {
                  debug: O || H,
                  commands: YY,
                  initialTools: [],
                  initialMessages: Dq ? [y3, Dq] : [y3],
                  mcpClients: [],
                  autoConnectIdeFlag: N,
                  mainThreadAgentDefinition: BA,
                  disableSlashCommands: a,
                  remoteSessionConfig: R7,
                  thinkingConfig: c5,
                }),
              ),
            );
            return;
          } else if (z6) {
            if (z6 === !0 || z6 === "") {
              (emitEvent("tengu_teleport_interactive_mode", {}),
                writeDebugLog("selectAndResumeTeleportTask: Starting teleport flow..."));
              let { TeleportResumeWrapper: H8 } = await Promise.resolve().then(
                  () => (oxq(), rxq),
                ),
                V8 = await jp(v4, (r8) =>
                  r$.default.createElement(H8, {
                    onComplete: r8,
                    onCancel: () => r8(null),
                    source: "cliArg",
                  }),
                );
              if (!V8) (await rq(0), process.exit(0));
              let { branchError: JA } = await zT6(V8.branch);
              O7 = YT6(V8.log, JA);
            } else if (typeof z6 === "string") {
              emitEvent("tengu_teleport_resume_session", { mode: "direct" });
              try {
                let H8 = await pM6(z6),
                  V8 = await hb8(H8);
                if (V8.status === "mismatch" || V8.status === "not_in_repo") {
                  let CA = V8.sessionRepo;
                  if (CA) {
                    let R7 = uNq(CA),
                      i4 = await mNq(R7);
                    if (i4.length > 0) {
                      let { TeleportRepoMismatchDialog: y3 } =
                          await Promise.resolve().then(() => (sxq(), axq)),
                        Dq = await jp(v4, (P5) =>
                          r$.default.createElement(y3, {
                            targetRepo: CA,
                            initialPaths: i4,
                            onSelectPath: P5,
                            onCancel: () => P5(null),
                          }),
                        );
                      if (Dq) (process.chdir(Dq), MH(Dq), LA6(Dq));
                      else await rq(0);
                    } else
                      throw new eD(
                        `You must run claude --teleport ${z6} from a checkout of ${CA}.`,
                        H1.red(`You must run claude --teleport ${z6} from a checkout of ${H1.bold(CA)}.
`),
                      );
                  }
                } else if (V8.status === "error")
                  throw new eD(
                    V8.errorMessage || "Failed to validate session",
                    H1.red(`Error: ${V8.errorMessage || "Failed to validate session"}
`),
                  );
                await ZV1();
                let { teleportWithProgress: JA } = await Promise.resolve().then(
                    () => (Kbq(), qbq),
                  ),
                  r8 = await JA(v4, z6);
                (Bk6({ sessionId: z6 }), (O7 = r8.messages));
              } catch (H8) {
                if (H8 instanceof eD)
                  process.stderr.write(
                    H8.formattedMessage +
                      `
`,
                  );
                else
                  (sendError(H8 instanceof Error ? H8 : Error(String(H8))),
                    process.stderr.write(
                      H1.red(`Error: ${H8 instanceof Error ? H8.message : String(H8)}
`),
                    ));
                await rq(1);
              }
            }
          }
          if (r6) {
            let H8 = r6;
            try {
              let V8 = performance.now(),
                JA = await j16(L1 ?? H8, void 0);
              if (!JA)
                return (
                  emitEvent("tengu_session_resumed", {
                    entrypoint: "cli_flag",
                    success: !1,
                  }),
                  await c86(v4, `No conversation found with session ID: ${H8}`)
                );
              let r8 = L1?.fullPath ?? JA.fullPath;
              if (
                ((U6 = await Pi8(
                  JA,
                  {
                    forkSession: !!$.forkSession,
                    sessionIdOverride: H8,
                    transcriptPath: r8,
                  },
                  g3,
                )),
                U6.restoredAgentDef)
              )
                BA = U6.restoredAgentDef;
              emitEvent("tengu_session_resumed", {
                entrypoint: "cli_flag",
                success: !0,
                resume_duration_ms: Math.round(performance.now() - V8),
              });
            } catch (V8) {
              (emitEvent("tengu_session_resumed", {
                entrypoint: "cli_flag",
                success: !1,
              }),
                sendError(V8 instanceof Error ? V8 : Error(String(V8))),
                await c86(v4, `Failed to resume session ${H8}`));
            }
          }
          if (S)
            try {
              let H8 = await S,
                V8 = H8.filter((JA) => !JA.success).length;
              if (V8 > 0)
                process.stderr.write(
                  H1.yellow(`Warning: ${V8}/${H8.length} file(s) failed to download.
`),
                );
            } catch (H8) {
              return await c86(
                v4,
                `Error downloading files: ${H8 instanceof Error ? H8.message : String(H8)}`,
              );
            }
          let E8 =
            U6 ??
            (Array.isArray(O7)
              ? {
                  messages: O7,
                  fileHistorySnapshots: void 0,
                  agentName: void 0,
                  agentColor: void 0,
                  restoredAgentDef: BA,
                  initialState: R5,
                }
              : void 0);
          if (E8) {
            if (Zu8(E8.messages)) W16();
            (gr8($),
              XS1($),
              await To6(
                v4,
                r$.default.createElement(
                  SY,
                  {
                    getFpsMetrics: a4,
                    stats: UA,
                    initialState: E8.initialState,
                  },
                  r$.default.createElement(_q, {
                    ...sY,
                    mainThreadAgentDefinition: E8.restoredAgentDef ?? BA,
                    initialMessages: E8.messages,
                    initialFileHistorySnapshots: E8.fileHistorySnapshots,
                    initialAgentName: E8.agentName,
                    initialAgentColor: E8.agentColor,
                  }),
                ),
              ));
          } else {
            let [H8, { ResumeConversation: V8 }] = await Promise.all([
              Ed(HA()),
              Promise.resolve().then(() => (zbq(), Ybq)),
            ]);
            await To6(
              v4,
              r$.default.createElement(
                SY,
                { getFpsMetrics: a4, stats: UA, initialState: R5 },
                r$.default.createElement(
                  hD,
                  null,
                  r$.default.createElement(V8, {
                    ...sY,
                    worktreePaths: H8,
                    initialSearchQuery: N1,
                    forkSession: $.forkSession,
                    filterByPr: U1,
                  }),
                ),
              ),
            );
          }
        } else {
          let F8 = fz && d5.length === 0 ? fz : void 0;
          (Bq("action_after_hooks"),
            gr8($),
            XS1($),
            await To6(
              v4,
              r$.default.createElement(
                SY,
                { getFpsMetrics: a4, stats: UA, initialState: R5 },
                r$.default.createElement(_q, {
                  ...sY,
                  initialMessages: d5.length > 0 ? d5 : void 0,
                  pendingHookMessages: F8,
                }),
              ),
            ));
        }
      })
      .version(
        `${{ ISSUES_EXPLAINER: "report the issue at https://github.com/anthropics/claude-code/issues", PACKAGE_URL: "klaudia", README_URL: "https://code.claude.com/docs/en/overview", VERSION: "2.1.66-klaudia", FEEDBACK_CHANNEL: "https://github.com/anthropics/claude-code/issues", BUILD_TIME: "2026-03-04T00:18:36Z" }.VERSION} (Klaudia)`,
        "-v, --version",
        "Output the version number",
      ),
    q.option(
      "-w, --worktree [name]",
      "Create a new git worktree for this session (optionally specify a name)",
    ),
    q.option(
      "--tmux",
      "Create a tmux session for the worktree (requires --worktree). Uses iTerm2 native panes when available; use --tmux=classic for traditional tmux.",
    ),
    q.addOption(new n3("--agent-id <id>", "Teammate agent ID").hideHelp()),
    q.addOption(
      new n3("--agent-name <name>", "Teammate display name").hideHelp(),
    ),
    q.addOption(
      new n3(
        "--team-name <name>",
        "Team name for swarm coordination",
      ).hideHelp(),
    ),
    q.addOption(
      new n3("--agent-color <color>", "Teammate UI color").hideHelp(),
    ),
    q.addOption(
      new n3(
        "--plan-mode-required",
        "Require plan mode before implementation",
      ).hideHelp(),
    ),
    q.addOption(
      new n3(
        "--parent-session-id <id>",
        "Parent session ID for analytics correlation",
      ).hideHelp(),
    ),
    q.addOption(
      new n3(
        "--teammate-mode <mode>",
        'How to spawn teammates: "tmux", "in-process", or "auto"',
      )
        .choices(["auto", "tmux", "in-process"])
        .hideHelp(),
    ),
    q.addOption(
      new n3(
        "--agent-type <type>",
        "Custom agent type for this teammate",
      ).hideHelp(),
    ),
    q.addOption(
      new n3(
        "--sdk-url <url>",
        "Use remote WebSocket endpoint for SDK I/O streaming (only with -p and stream-json format)",
      ).hideHelp(),
    ),
    q.addOption(
      new n3(
        "--teleport [session]",
        "Resume a teleport session, optionally specify session ID",
      ).hideHelp(),
    ),
    q.addOption(
      new n3(
        "--remote [description]",
        "Create a remote session with the given description",
      ).hideHelp(),
    ));
  let K = q
    .command("mcp")
    .description("Configure and manage MCP servers")
    .helpOption("-h, --help", "Display help for command")
    .configureHelp(A())
    .enablePositionalOptions();
  (K.command("serve")
    .description("Start the Claude Code MCP server")
    .helpOption("-h, --help", "Display help for command")
    .option("-d, --debug", "Enable debug mode", () => !0)
    .option("--verbose", "Override verbose mode setting from config", () => !0)
    .action(async ({ debug: _, verbose: $ }) => {
      let { mcpServeHandler: O } = await Promise.resolve().then(
        () => (d86(), U86),
      );
      await O({ debug: _, verbose: $ });
    }),
    DNq(K),
    K.command("remove <name>")
      .description("Remove an MCP server")
      .option(
        "-s, --scope <scope>",
        "Configuration scope (local, user, or project) - if not specified, removes from whichever scope it exists in",
      )
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { mcpRemoveHandler: O } = await Promise.resolve().then(
          () => (d86(), U86),
        );
        await O(_, $);
      }),
    K.command("list")
      .description("List configured MCP servers")
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let { mcpListHandler: _ } = await Promise.resolve().then(
          () => (d86(), U86),
        );
        await _();
      }),
    K.command("get <name>")
      .description("Get details about an MCP server")
      .helpOption("-h, --help", "Display help for command")
      .action(async (_) => {
        let { mcpGetHandler: $ } = await Promise.resolve().then(
          () => (d86(), U86),
        );
        await $(_);
      }),
    K.command("add-json <name> <json>")
      .description("Add an MCP server (stdio or SSE) with a JSON string")
      .option(
        "-s, --scope <scope>",
        "Configuration scope (local, user, or project)",
        "local",
      )
      .option(
        "--client-secret",
        "Prompt for OAuth client secret (or set MCP_CLIENT_SECRET env var)",
      )
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $, O) => {
        let { mcpAddJsonHandler: H } = await Promise.resolve().then(
          () => (d86(), U86),
        );
        await H(_, $, O);
      }),
    K.command("add-from-claude-desktop")
      .description("Import MCP servers from Claude Desktop (Mac and WSL only)")
      .option(
        "-s, --scope <scope>",
        "Configuration scope (local, user, or project)",
        "local",
      )
      .helpOption("-h, --help", "Display help for command")
      .action(async (_) => {
        let { mcpAddFromDesktopHandler: $ } = await Promise.resolve().then(
          () => (d86(), U86),
        );
        await $(_);
      }),
    K.command("reset-project-choices")
      .description(
        "Reset all approved and rejected project-scoped (.mcp.json) servers within this project",
      )
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let { mcpResetChoicesHandler: _ } = await Promise.resolve().then(
          () => (d86(), U86),
        );
        await _();
      }));
  let Y = q
    .command("auth")
    .description("Manage authentication")
    .helpOption("-h, --help", "Display help for command")
    .configureHelp(A());
  (Y.command("login")
    .description("Sign in to your Anthropic account")
    .option("--email <email>", "Pre-populate email address on the login page")
    .option("--sso", "Force SSO login flow")
    .helpOption("-h, --help", "Display help for command")
    .action(async ({ email: _, sso: $ }) => {
      let { authLogin: O } = await Promise.resolve().then(() => (zc6(), QT1));
      await O({ email: _, sso: $ });
    }),
    Y.command("status")
      .description("Show authentication status")
      .option("--json", "Output as JSON (default)")
      .option("--text", "Output as human-readable text")
      .helpOption("-h, --help", "Display help for command")
      .action(async (_) => {
        let { authStatus: $ } = await Promise.resolve().then(
          () => (zc6(), QT1),
        );
        await $(_);
      }),
    Y.command("logout")
      .description("Log out from your Anthropic account")
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let { authLogout: _ } = await Promise.resolve().then(
          () => (zc6(), QT1),
        );
        await _();
      }));
  let z = q
    .command("plugin")
    .description("Manage Claude Code plugins")
    .helpOption("-h, --help", "Display help for command")
    .configureHelp(A());
  (z
    .command("validate <path>")
    .description("Validate a plugin or marketplace manifest")
    .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
    .helpOption("-h, --help", "Display help for command")
    .action(async (_, $) => {
      let { pluginValidateHandler: O } = await Promise.resolve().then(
        () => (tC(), sC),
      );
      await O(_, $);
    }),
    z
      .command("list")
      .description("List installed plugins")
      .option("--json", "Output as JSON")
      .option(
        "--available",
        "Include available plugins from marketplaces (requires --json)",
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_) => {
        let { pluginListHandler: $ } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await $(_);
      }));
  let w = z
    .command("marketplace")
    .description("Manage Claude Code marketplaces")
    .helpOption("-h, --help", "Display help for command")
    .configureHelp(A());
  (w
    .command("add <source>")
    .description("Add a marketplace from a URL, path, or GitHub repo")
    .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
    .option(
      "--sparse <paths...>",
      "Limit checkout to specific directories via git sparse-checkout (for monorepos). Example: --sparse .claude-plugin plugins",
    )
    .option(
      "--scope <scope>",
      "Where to declare the marketplace: user (default), project, or local",
    )
    .helpOption("-h, --help", "Display help for command")
    .action(async (_, $) => {
      let { marketplaceAddHandler: O } = await Promise.resolve().then(
        () => (tC(), sC),
      );
      await O(_, $);
    }),
    w
      .command("list")
      .description("List all configured marketplaces")
      .option("--json", "Output as JSON")
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_) => {
        let { marketplaceListHandler: $ } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await $(_);
      }),
    w
      .command("remove <name>")
      .alias("rm")
      .description("Remove a configured marketplace")
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { marketplaceRemoveHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    w
      .command("update [name]")
      .description(
        "Update marketplace(s) from their source - updates all if no name specified",
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { marketplaceUpdateHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    z
      .command("install <plugin>")
      .alias("i")
      .description(
        "Install a plugin from available marketplaces (use plugin@marketplace for specific marketplace)",
      )
      .option(
        "-s, --scope <scope>",
        "Installation scope: user, project, or local",
        "user",
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { pluginInstallHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    z
      .command("uninstall <plugin>")
      .alias("remove")
      .alias("rm")
      .description("Uninstall an installed plugin")
      .option(
        "-s, --scope <scope>",
        "Uninstall from scope: user, project, or local",
        "user",
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { pluginUninstallHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    z
      .command("enable <plugin>")
      .description("Enable a disabled plugin")
      .option(
        "-s, --scope <scope>",
        `Installation scope: ${wW.join(", ")} (default: auto-detect)`,
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { pluginEnableHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    z
      .command("disable [plugin]")
      .description("Disable an enabled plugin")
      .option("-a, --all", "Disable all enabled plugins")
      .option(
        "-s, --scope <scope>",
        `Installation scope: ${wW.join(", ")} (default: auto-detect)`,
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { pluginDisableHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    z
      .command("update <plugin>")
      .description(
        "Update a plugin to the latest version (restart required to apply)",
      )
      .option(
        "-s, --scope <scope>",
        `Installation scope: ${J26.join(", ")} (default: user)`,
      )
      .addOption(new n3("--cowork", "Use cowork_plugins directory").hideHelp())
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { pluginUpdateHandler: O } = await Promise.resolve().then(
          () => (tC(), sC),
        );
        await O(_, $);
      }),
    q
      .command("setup-token")
      .description(
        "Set up a long-lived authentication token (requires Claude subscription)",
      )
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let [{ setupTokenHandler: _ }, { createRoot: $ }] = await Promise.all([
            Promise.resolve().then(() => (JS1(), jS1)),
            Promise.resolve().then(() => (Q6(), oI6)),
          ]),
          O = await $(Z66(!1));
        await _(O);
      }),
    q
      .command("agents")
      .description("List configured agents")
      .helpOption("-h, --help", "Display help for command")
      .option(
        "--setting-sources <sources>",
        "Comma-separated list of setting sources to load (user, project, local).",
      )
      .action(async () => {
        let { agentsHandler: _ } = await Promise.resolve().then(
          () => (Vbq(), Nbq),
        );
        (await _(), process.exit(0));
      }));
  {
    let { isBridgeEnabled: _ } = await Promise.resolve().then(
      () => (Li(), ig8),
    );
    q.command("remote-control", { hidden: !_() })
      .alias("rc")
      .description(
        "Connect your local environment for remote-control sessions via claude.ai/code",
      )
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let { bridgeMain: $ } = await Promise.resolve().then(
          () => (gl8(), Bl8),
        );
        await $(process.argv.slice(3));
      });
  }
  return (
    q
      .command("doctor")
      .description("Check the health of your Claude Code auto-updater")
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let [{ doctorHandler: _ }, { createRoot: $ }] = await Promise.all([
            Promise.resolve().then(() => (JS1(), jS1)),
            Promise.resolve().then(() => (Q6(), oI6)),
          ]),
          O = await $(Z66(!1));
        await _(O);
      }),
    q
      .command("update")
      .alias("upgrade")
      .description("Check for updates and install if available")
      .helpOption("-h, --help", "Display help for command")
      .action(async () => {
        let { update: _ } = await Promise.resolve().then(() => (kbq(), vbq));
        await _();
      }),
    q
      .command("install [target]")
      .description(
        "Install Claude Code native build. Use [target] to specify version (stable, latest, or specific version)",
      )
      .option("--force", "Force installation even if already installed")
      .helpOption("-h, --help", "Display help for command")
      .action(async (_, $) => {
        let { installHandler: O } = await Promise.resolve().then(
          () => (JS1(), jS1),
        );
        await O(_, $);
      }),
    Bq("run_before_parse"),
    await q.parseAsync(process.argv),
    Bq("run_after_parse"),
    Bq("main_after_run"),
    ik6(),
    q
  );
}
async function ZCz({
  hasInitialPrompt: A,
  hasStdin: q,
  verbose: K,
  debug: Y,
  debugToStderr: z,
  print: w,
  outputFormat: _,
  inputFormat: $,
  numAllowedTools: O,
  numDisallowedTools: H,
  mcpClientCount: j,
  worktreeEnabled: J,
  skipWebFetchPreflight: D,
  githubActionInputs: X,
  dangerouslySkipPermissionsPassed: M,
  permissionMode: P,
  modeIsBypass: W,
  allowDangerouslySkipPermissionsPassed: G,
  systemPromptFlag: Z,
  appendSystemPromptFlag: f,
  thinkingConfig: N,
}) {
  try {
    emitEvent("tengu_init", {
      entrypoint: "claude",
      hasInitialPrompt: A,
      hasStdin: q,
      verbose: K,
      debug: Y,
      debugToStderr: z,
      print: w,
      outputFormat: _,
      inputFormat: $,
      numAllowedTools: O,
      numDisallowedTools: H,
      mcpClientCount: j,
      worktree: J,
      skipWebFetchPreflight: D,
      ...(X && { githubActionInputs: X }),
      dangerouslySkipPermissionsPassed: M,
      permissionMode: P,
      modeIsBypass: W,
      allowDangerouslySkipPermissionsPassed: G,
      thinkingType: N.type,
      ...(Z && { systemPromptFlag: Z }),
      ...(f && { appendSystemPromptFlag: f }),
      is_simple: isTruthy(process.env.CLAUDE_CODE_SIMPLE) || void 0,
      is_coordinator: void 0,
      autoUpdatesChannel: U7().autoUpdatesChannel ?? "latest",
      ...{},
    });
  } catch (V) {
    sendError(V instanceof Error ? V : Error(String(V)));
  }
}
function gr8(A) {}
function XS1(A) {}
function fCz() {
  (process.stderr.isTTY
    ? process.stderr
    : process.stdout.isTTY
      ? process.stdout
      : void 0
  )?.write(hh);
}
function TCz(A) {
  if (typeof A !== "object" || A === null) return {};
  let q = A,
    K = q.teammateMode;
  return {
    agentId: typeof q.agentId === "string" ? q.agentId : void 0,
    agentName: typeof q.agentName === "string" ? q.agentName : void 0,
    teamName: typeof q.teamName === "string" ? q.teamName : void 0,
    agentColor: typeof q.agentColor === "string" ? q.agentColor : void 0,
    planModeRequired:
      typeof q.planModeRequired === "boolean" ? q.planModeRequired : void 0,
    parentSessionId:
      typeof q.parentSessionId === "string" ? q.parentSessionId : void 0,
    teammateMode:
      K === "auto" || K === "tmux" || K === "in-process" ? K : void 0,
    agentType: typeof q.agentType === "string" ? q.agentType : void 0,
  };
}
var r$,
  Lbq = () => (oz(), oX(vG8)),
  tRz = () => oX(Qo4),
  eRz = () => (of6(), oX(Qx8)),
  ACz = null;
var hbq = E(() => {
  kS();
  Ic8();
  Qh();
  uI6();
  kA();
  r1();
  jTq();
  F7();
  Wr6();
  Cm();
  PR1();
  WTq();
  mTq();
  K3();
  R81();
  m9();
  uk();
  zY1();
  el8();
  kZ6();
  t16();
  tf();
  IZ6();
  oP();
  Rg();
  VY();
  Vr6();
  IA();
  sl6();
  l8();
  Hs();
  GG();
  iK();
  nG1();
  rh();
  r1();
  Ai8();
  zX();
  NI();
  h1();
  Vq();
  Cl();
  HP();
  qi8();
  t4();
  qc6();
  yA();
  Ry1();
  NO();
  nf();
  N8();
  B1();
  Sz6();
  Ki8();
  y26();
  zu6();
  t3();
  ANq();
  lw();
  UR();
  NX();
  Vq();
  XF8();
  ah();
  F7();
  u1();
  Jr6();
  AK6();
  yP();
  _Nq();
  ll();
  KT6();
  wi8();
  Hi8();
  c0();
  jE();
  yu();
  ne();
  U_();
  ji8();
  EI();
  XNq();
  Ok8();
  CG();
  Di8();
  g96();
  rI();
  Vz();
  I16();
  J7();
  f1();
  R_();
  $7();
  hw();
  kr6();
  N$();
  UI();
  B1();
  GNq();
  fNq();
  NNq();
  vNq();
  kNq();
  LNq();
  RNq();
  SNq();
  Zi8();
  P16();
  Gz6();
  RA();
  RR1();
  Uv();
  r2();
  fi8();
  $j();
  UN6();
  nz();
  Hi();
  bN();
  S16();
  a76();
  GF();
  r$ = Y6(W6(), 1);
  Bq("main_tsx_entry");
  sGq();
  Bq("main_tsx_imports_loaded");
  if (isDebuggerAttached()) process.exit(1);
});
process.env.COREPACK_ENABLE_AUTO_PIN = "0";
if (process.env.CLAUDE_CODE_REMOTE === "true") {
  let A = process.env.NODE_OPTIONS || "";
  process.env.NODE_OPTIONS = A
    ? `${A} --max-old-space-size=8192`
    : "--max-old-space-size=8192";
}
async function VCz() {
  let A = process.argv.slice(2);
  if (
    A.length === 1 &&
    (A[0] === "--version" || A[0] === "-v" || A[0] === "-V")
  ) {
    console.log(
      `${{ ISSUES_EXPLAINER: "report the issue at https://github.com/anthropics/claude-code/issues", PACKAGE_URL: "klaudia", README_URL: "https://code.claude.com/docs/en/overview", VERSION: "2.1.66-klaudia", FEEDBACK_CHANNEL: "https://github.com/anthropics/claude-code/issues", BUILD_TIME: "2026-03-04T00:18:36Z" }.VERSION} (Klaudia)`,
    );
    return;
  }
  let { profileCheckpoint: q } = await Promise.resolve().then(
    () => (kS(), Te8),
  );
  if ((q("cli_entry"), A[0] === "--ripgrep")) {
    q("cli_ripgrep_path");
    let w = A.slice(1),
      { ripgrepMain: _ } = await Promise.resolve().then(() => (Ve8(), Ne8));
    process.exitCode = _(w);
    return;
  }
  if (process.argv[2] === "--claude-in-chrome-mcp") {
    q("cli_claude_in_chrome_mcp_path");
    let { runClaudeInChromeMcpServer: w } = await Promise.resolve().then(
      () => (RV8(), yV8),
    );
    await w();
    return;
  } else if (process.argv[2] === "--chrome-native-host") {
    q("cli_chrome_native_host_path");
    let { runChromeNativeHost: w } = await Promise.resolve().then(
      () => (xfq(), Ifq),
    );
    await w();
    return;
  }
  if (
    A[0] === "remote-control" ||
    A[0] === "rc" ||
    A[0] === "remote" ||
    A[0] === "sync" ||
    A[0] === "bridge"
  ) {
    q("cli_bridge_path");
    let { enableConfigs: w } = await Promise.resolve().then(() => (l8(), $r6));
    w();
    let { isBridgeEnabledBlocking: _, checkBridgeMinVersion: $ } =
        await Promise.resolve().then(() => (Li(), ig8)),
      { BRIDGE_LOGIN_ERROR: O } = await Promise.resolve().then(() => B0q),
      { bridgeMain: H } = await Promise.resolve().then(() => (gl8(), Bl8)),
      { getClaudeAIOAuthTokens: j } = await Promise.resolve().then(
        () => (IA(), lN6),
      );
    if (!j()?.accessToken) (console.error(O), process.exit(1));
    if (!(await _()))
      (console.error(
        "Error: Remote Control is not yet enabled for your account.",
      ),
        process.exit(1));
    let J = $();
    if (J) (console.error(J), process.exit(1));
    let { waitForPolicyLimitsToLoad: D, isPolicyAllowed: X } =
      await Promise.resolve().then(() => (tf(), Iy4));
    if ((await D(), !X("allow_remote_sessions")))
      (console.error(
        "Error: Remote Control sessions are disabled by your organization's policy.",
      ),
        process.exit(1));
    await H(A.slice(1));
    return;
  }
  if (
    (A.includes("--tmux") || A.includes("--tmux=classic")) &&
    (A.includes("-w") ||
      A.includes("--worktree") ||
      A.some((w) => w.startsWith("--worktree=")))
  ) {
    q("cli_tmux_worktree_fast_path");
    let { enableConfigs: w } = await Promise.resolve().then(() => (l8(), $r6));
    w();
    let { isWorktreeModeEnabled: _ } = await Promise.resolve().then(() => eqq);
    if (_()) {
      let { execIntoTmuxWorktree: $ } = await Promise.resolve().then(
          () => (GF(), ta4),
        ),
        O = await $(A);
      if (O.handled) return;
      if (O.error) (console.error(O.error), process.exit(1));
    }
  }
  if (A.length === 1 && (A[0] === "--update" || A[0] === "--upgrade"))
    process.argv = [process.argv[0], process.argv[1], "update"];
  if (
    process.env.CLAUDECODE === "1" &&
    !A.some((w) => w.startsWith("--team-name")) &&
    !kCz(A)
  )
    (console.error(`Error: Claude Code cannot be launched inside another Claude Code session.
Nested sessions share runtime resources and will crash all active sessions.
To bypass this check, unset the CLAUDECODE environment variable.`),
      process.exit(1));
  let { startCapturingEarlyInput: Y } = await Promise.resolve().then(
    () => (uI6(), tJ7),
  );
  (Y(), q("cli_before_main_import"));
  let { main: z } = await Promise.resolve().then(() => (hbq(), Sbq));
  (q("cli_after_main_import"), await z(), q("cli_after_main_complete"));
}
var vCz = [
  "plugin",
  "mcp",
  "auth",
  "doctor",
  "update",
  "install",
  "rollback",
  "log",
  "completion",
];
function kCz(A) {
  if (A.includes("--help") || A.includes("-h")) return !0;
  let q = A.find((K) => !K.startsWith("-"));
  return q !== void 0 && vCz.includes(q);
}
VCz();
