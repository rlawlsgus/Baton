# Phase 0 spikes

Verifies the assumptions behind the account-switching launcher design **without any real
Claude account**. Real `claude.exe` runs against a local mock of the Messages API; each
dummy API key plays the role of one account.

```
harness/mockapi.py   mock Messages API (SSE, 429 with unified rate-limit headers, tool_use, slow streams)
harness/conpty.py    ctypes ConPTY driver: gives claude.exe a real console, scripts keystrokes, reads output
harness/cc.py        isolated env + test profiles (CLAUDE_CONFIG_DIR), transcript helpers, junctions
smoke.py             print mode + interactive TUI round trip against the mock
experiments/         one script per Phase 0 question; each prints a JSON report
RESULTS.md           findings
```

Run: `python -I spikes/phase0/smoke.py <empty run dir>`

## Isolation rules (all scripts)

- Always go through `cc.Run`: it gives claude a fake `USERPROFILE`/`HOME`, a per-profile
  `CLAUDE_CONFIG_DIR`, and an allowlisted env (no `ANTHROPIC_*` / `CLAUDE_CODE_*` from the
  host). Never read or write the real `~/.claude`, `~/.claude.json` or `Claude-Data`.
- Auth is `apiKeyHelper: echo sk-mock-X` and `ANTHROPIC_BASE_URL=http://127.0.0.1:<mock>`
  in the profile's `settings.json`. Never use real credentials; never run
  `claude auth login/logout` or `setup-token`.
- Only kill processes you started (by pid). The desktop app runs the same `claude.exe`, so
  never `taskkill /im claude.exe`.

## Harness cheat sheet

```python
import os, sys
sys.path.insert(0, r"...\spikes\phase0\harness")
from cc import Run, read_jsonl, summarize_rows, transcripts, session_ids, junction, fwd, short_path, wait_until
from mockapi import MockServer, write_scenario, read_log

run = Run(root)                                  # root: an empty dir only you use
scen = os.path.join(run.root, "scenario.json")
write_scenario(scen, {"sk-mock-A": {"mode": "echo"}, "sk-mock-B": {"mode": "echo"}})
srv = MockServer(os.path.join(run.logs, "mock.jsonl"), scen); srv.start()
a = run.profile("A", "sk-mock-A", srv.base_url, trust=[run.proj])   # seeds onboarding + trust
cp = run.run_print(a, ["-p", "hi", "--output-format", "json"])         # pipes, -p mode
pc = run.pty(a, ["--session-id", sid], name="tui"); pc.start()        # interactive TUI in ConPTY
pc.wait_for(r"auto mode on|for shortcuts|>\s", 60); time.sleep(1.5)
m = pc.mark(); pc.submit("hello"); pc.wait_for(r"MOCK-REPLY\[A\]", 60, since=m)
pc.send("\x03"); time.sleep(0.5); pc.send("\x03"); pc.wait_exit(15); pc.close()
write_scenario(scen, {"sk-mock-A": {"mode": "limit"}})                 # flip an account at runtime
```

Observed in the smoke test (claude 2.1.308):
- TUI is ready in ~3-6 s; header shows `API Usage Billing`; the input row is `>`; the footer
  says `auto mode on`. Wait ~1.5 s after it appears before typing.
- Default permission mode is **auto** (a classifier checks tool calls, which sends extra
  requests to the mock). For tool tests pass `--permission-mode default` (profile settings
  already allow Bash/PowerShell/Read/Write/Edit) and say what you saw.
- Ctrl-C twice exits and prints `claude --resume <id>`.
- A title-generation side request (no tools) follows the first prompt; in the mock log
  `main_loop: true` marks the real agent-loop requests.
- Transcript rows include new types (`api-request-blob`, `api-request`, `cost-state`,
  `atis-latch`, `last-prompt`) besides `user`/`assistant`.
