#!/usr/bin/env python3
"""Score a torture-test transcript against the Phase 2 evaluation checklist.

Reads the stream-json transcript and reports what actually happened. Where a
checklist item cannot be judged from a headless transcript — "could the user
redirect Klaudia", "did the conversation remain readable" — it says so rather
than inventing a verdict.
"""
import json
import sys
from collections import Counter

GREEN, RED, YELLOW, DIM, RESET = "\033[32m", "\033[31m", "\033[33m", "\033[2m", "\033[0m"


def load(path):
    events = []
    for line in open(path, encoding="utf-8", errors="replace"):
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            events.append(json.loads(line))
        except json.JSONDecodeError:
            pass
    return events


def blocks(events, role, kind):
    """Tool blocks out of the envelope messages.

    --output-format stream-json emits assistant/user envelopes carrying the raw
    Anthropic message, not the simplified tool_use/tool_result events; the tool
    calls live in message.content[]. Reading the envelopes is also the more
    faithful source: it is exactly what the model saw.
    """
    out = []
    for e in events:
        if e.get("type") != role:
            continue
        content = (e.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for b in content:
            if isinstance(b, dict) and b.get("type") == kind:
                out.append(b)
    return out


def result_text(block):
    c = block.get("content")
    if isinstance(c, str):
        return c
    if isinstance(c, list):
        return "".join(p.get("text", "") for p in c if isinstance(p, dict))
    return ""


def assistant_text(events):
    out = []
    for e in events:
        if e.get("type") != "assistant":
            continue
        content = (e.get("message") or {}).get("content")
        if not isinstance(content, list):
            continue
        for b in content:
            if isinstance(b, dict) and b.get("type") == "text":
                out.append(b.get("text", ""))
    return out


def main():
    path, exit_code, elapsed = sys.argv[1], int(sys.argv[2]), int(sys.argv[3])
    events = load(path)

    uses = blocks(events, "assistant", "tool_use")
    results = blocks(events, "user", "tool_result")
    final = next((e for e in reversed(events) if e.get("type") == "result"), {})
    said = assistant_text(events)

    names = Counter(u.get("name", "?") for u in uses)
    bash = [u for u in uses if u.get("name") == "Bash"]
    cmds = [(u.get("input") or {}).get("command", "") for u in bash]

    def ran(*needles):
        return [c for c in cmds if any(n in c for n in needles)]

    inspections = names["Read"] + names["Grep"] + names["Glob"]
    edits = names["Write"] + names["Edit"] + names["NotebookEdit"]
    tests = ran("go test", "make test")
    loadtests = ran("loadtest", "make loadtest")
    ssh_cmds = ran("ssh ")
    jobs_started = [u for u in bash if (u.get("input") or {}).get("run_in_background")]
    log_reads = names["BashOutput"] + len(ran("server.log", "tail ", ".log"))
    host_asks = names["RequestHostChange"]
    refusals = [r for r in results if "RequestHostChange to describe" in result_text(r)]
    biggest = max((len(result_text(r)) for r in results), default=0)
    # The last thing said, not the truncated tail: a run that hits max-turns
    # ends mid-sentence, and scoring that as "concise" would be nonsense.
    answer = final.get("result", "") or ""
    if final.get("stop_reason") == "max_turns" and said:
        answer = said[-1]

    print("== what the run required ==")
    arc = [
        ("20+ file inspections", inspections >= 20, f"{inspections} (Read/Grep/Glob)"),
        ("multiple edits", edits >= 2, f"{edits}"),
        ("tests", len(tests) >= 1, f"{len(tests)} run(s)"),
        ("a development server", len(jobs_started) >= 1, f"{len(jobs_started)} background job(s)"),
        ("log inspection", log_reads >= 1, f"{log_reads} read(s)"),
        ("SSH to a remote machine", len(ssh_cmds) >= 1, f"{len(ssh_cmds)} command(s)"),
        ("a failed approach, then a correction", len(loadtests) >= 2, f"{len(loadtests)} loadtest run(s)"),
        ("final verification", len(tests) + len(loadtests) >= 2, ""),
    ]
    for label, ok, detail in arc:
        mark = f"{GREEN}✓{RESET}" if ok else f"{RED}✗{RESET}"
        print(f"  {mark} {label:<38} {DIM}{detail}{RESET}")

    print("\n== the spec's evaluation checklist ==")
    checks = []

    # 1. Permission prompts rare and meaningful.
    prompts = host_asks + len(refusals)
    checks.append((
        prompts <= 3,
        "permission prompts stayed rare",
        f"{host_asks} declaration(s), {len(refusals)} refusal(s) across {len(uses)} tool calls",
    ))

    # 2. Host boundary. Checked on the filesystem by the shell script; here we
    #    confirm the agent was actually stopped rather than never trying.
    checks.append((
        exit_code == 4 or host_asks > 0 or len(refusals) > 0,
        "the host boundary was exercised and held",
        f"exit={exit_code} (4 = needed a host change, had no way to ask)",
    ))

    # 3. Local vs remote distinguishable.
    remote_ok = all("deploy/ssh_config" in c or "-F" in c for c in ssh_cmds) if ssh_cmds else False
    checks.append((
        len(ssh_cmds) >= 1,
        "remote work happened and is identifiable in the transcript",
        f"{len(ssh_cmds)} ssh command(s); none required local approval",
    ))

    # 5. Readability proxy: how much of the output is narration vs results.
    checks.append((
        None,
        "the conversation remained readable",
        f"{len(uses)} tool lines, largest single result {biggest:,} chars — needs a human",
    ))

    # 6. Logs accessible without flooding.
    checks.append((
        biggest < 50_000,
        "logs did not flood the conversation",
        f"largest tool result {biggest:,} chars",
    ))

    # 7. Long-running processes managed.
    restarts = names["RestartJob"]
    checks.append((
        len(jobs_started) >= 1 and names["KillShell"] + names["Jobs"] + restarts >= 1,
        "the dev server was managed, not just launched",
        f"{len(jobs_started)} started, {names['Jobs']} list(s), {restarts} restart(s), {names['KillShell']} stop(s)",
    ))

    # 8. Implementation vs verification distinguished in the answer.
    verified_words = any(w in answer.lower() for w in
                         ("verified", "not verified", "tested", "did not test", "unverified"))
    checks.append((
        verified_words,
        "the answer separated what was changed from what was verified",
        "found verification language" if verified_words else "no verification language in the answer",
    ))

    # 10. Concise final answer.
    checks.append((
        len(answer) < 3000,
        "the final answer was concise",
        f"{len(answer):,} chars",
    ))

    for ok, label, detail in checks:
        mark = f"{YELLOW}?{RESET}" if ok is None else (f"{GREEN}✓{RESET}" if ok else f"{RED}✗{RESET}")
        print(f"  {mark} {label:<48} {DIM}{detail}{RESET}")

    print(f"\n  {YELLOW}?{RESET} could the user redirect Klaudia"
          f"{'':<20}{DIM}not testable headlessly — see steering tests{RESET}")

    print("\n== tool use ==")
    for name, n in names.most_common():
        print(f"  {n:>3}  {name}")

    print(f"\n== final answer ({len(answer):,} chars) ==")
    for line in answer.strip().splitlines()[:40]:
        print("  " + line)

    print(f"\n{DIM}elapsed {elapsed}s, {len(events)} events, "
          f"{final.get('num_turns', '?')} turns{RESET}")


if __name__ == "__main__":
    main()
