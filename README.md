# baton

Keep a Claude Code session going across several of **your own** Claude accounts.
When the account in use hits its usage limit, baton stops Claude Code cleanly and
continues the *same conversation* on your next logged-in account — no typing needed.

Windows 10/11 (x64, arm64). A window for everyday use (`baton-app.exe`) and the same
features on the command line (`baton.exe`). Nothing else to install: baton uses the Claude
Code that comes with the Claude desktop app.
Status: **0.2 preview** — works end to end against a mock API; real-account use is the next step.

## Claude 데스크톱 앱 사용자: 시작하기

1. `baton_<버전>_windows_amd64.zip`을 받아 **압축을 풉니다** (zip에서 마우스 오른쪽 → 모두 압축 풀기).
   ARM 노트북(Snapdragon 등)은 `..._arm64.zip`을 받습니다.
2. 풀린 폴더의 **baton-app.exe**를 엽니다. "Windows의 PC 보호" 창이 뜨면 **추가 정보 → 실행**을 누릅니다
   (서명되지 않은 프로그램이라 처음에 나오는 안내입니다).
3. 처음 화면에서 안내를 읽고 전환 방식(권장: 전환 전 확인)을 고릅니다.
4. **계정 추가**: 계정마다 한 번 로그인합니다(2단계 인증 포함). 로그인은 그 계정에 계속 유지되고, 전환할 때
   다시 로그인하지 않습니다. 두 번째 계정부터는 브라우저가 이미 첫 계정으로 로그인되어 있을 수 있으니,
   화면 안내대로 InPrivate 창에서 로그인하세요. 평소 브라우저의 로그인은 그대로 둡니다.
5. **새 세션 시작**: 작업할 폴더를 고르면 Claude Code가 별도의 창에서 열립니다. 작업은 그 창에서 합니다.
   한도에 걸리면 같은 대화가 다음 계정에서 이어집니다. baton 창은 닫아도 세션은 계속됩니다.
6. 도움말 탭의 **시작 메뉴에 설치**를 누르면 시작 메뉴에서 열 수 있고, 작업 표시줄 고정도 유지됩니다.

Claude 데스크톱 앱 자체의 로그인은 바뀌지 않습니다. baton의 계정들은 데스크톱 앱과 따로 로그인되어 있습니다.

## For Claude desktop app users (English)

Unzip the release, open **baton-app.exe** (on "Windows protected your PC", choose *More info →
Run anyway*), read the notice and pick a switch mode, add each account (one sign-in each,
including two-factor authentication; from the second account on, sign in in an InPrivate
window as the window explains), then **Start a session** in a project folder. Claude Code
opens in its own window; work there. *Help → Install to the Start menu* adds baton to the
Start menu.

## How it works

- Every account is its own Claude Code profile (`CLAUDE_CONFIG_DIR`), signed in and
  refreshed **by Claude Code itself**. baton never reads, stores or sends credentials.
- `baton run` starts the real interactive `claude.exe` in your console and passes it a
  settings file with hooks. When a turn ends on a usage limit, the `StopFailure` hook
  tells baton.
- baton waits until Claude Code is idle, stops it with Ctrl+Break (Claude saves the
  transcript and exits normally), and restarts it on the next account with
  `claude -r "<transcript>" "<continue prompt>"`. The session id and transcript file
  stay the same; the new account receives the full conversation, including the prompt
  that hit the limit.
- A switch takes about 5–10 seconds plus normal API time.
- The window (`baton-app.exe`) is a small local web page shown in a WebView2 window of
  baton's own (the Edge WebView2 runtime ships with Windows 11 and almost all Windows 10;
  without it the page opens in your default browser). It reads baton's files, changes
  settings and the account order itself, and runs `baton.exe` for sign-ins, sessions and
  every other account change, so the window and the command line follow the same rules.
  The page is served on 127.0.0.1 only, to that window only: a one-time code in the window's
  address is exchanged for a token the page sends in a header with every request (no
  cookies, which a browser would send to every local port), plus same-origin checks.

## Install

Download `baton_<version>_windows_amd64.zip` (most PCs) or `..._arm64.zip` (ARM PCs such
as Snapdragon laptops), check it against `checksums.txt`
(`Get-FileHash <zip> -Algorithm SHA256`) and unzip it. It contains `baton-app.exe` (the
window), `baton.exe` (the command line; keep both in the same folder), this README and the
licenses. *Help → Install to the Start menu* in the window copies them to
`%LOCALAPPDATA%\Programs\baton`; for the command line, put that folder (or the unzipped
one) on your `PATH`.

Claude Code: baton uses the Claude Code CLI if it is installed, and otherwise the copy
that comes with the Claude desktop app (it appears after the app's Code tab has been opened
once; `baton config allow_desktop_bundle false` turns that off). To install the CLI:

```bash
powershell -c "irm https://claude.ai/install.ps1 | iex"
```

Or build from source (Go 1.27+):

```bash
go build -trimpath -o bin/baton.exe ./cmd/baton
```

```bash
go build -trimpath -ldflags "-H windowsgui -X main.guiBuild=1" -o bin/baton-app.exe ./cmd/baton
```

or `powershell -NoProfile -ExecutionPolicy Bypass -File scripts\build.ps1 -Version 0.2.0`
for release zips (x64 and arm64, with checksums; `dist\` then holds only those).
`go run ./tools/winres -version <v>` regenerates the icon, manifest and version resources.

## Quick start

```bash
baton add personal        # opens Claude's sign-in for this account
baton add work            # repeat for each account you own
baton list
baton run                 # start Claude Code under baton
```

```
> baton run
  ... you work in Claude Code as usual ...
  ... the account hits its limit ...
baton: usage limit on "personal" (resets 15:00) -> continuing on "work"
  ... the same session continues on "work", with the whole conversation ...
```

On the first `baton run` you choose a switch mode: `auto` (switch without asking),
`confirm` (ask before each switch) or `off` (only `baton switch`).

Pass Claude Code options (and a first prompt) after `--`; to choose the account, use
`--account`:

```bash
baton run -- --model opus --permission-mode plan
baton run --account work -- -r "C:\path\to\session.jsonl"
```

Options you pass are kept on every switch, except start-only ones: which session to
open (`-r`, `-c`, `--session-id`, `--fork-session`) and `--file`. The permission mode
follows the session: after a switch, baton starts the next account in the mode the
session was last in (Claude Code itself would fall back to the account's default). If you
started with `--dangerously-skip-permissions` and accepted Claude Code's warning once on
any account, baton carries that acceptance to the next account so the session does not
stop on the warning; if you left bypass mode meanwhile (Shift+Tab), the next account starts
in the mode you were in, with bypass still available. Put your prompt before options that take several values, such as
`--add-dir a b`, because Claude Code reads every following word as one of their values.

## Commands

| Command | What it does |
|---|---|
| `baton gui` | open the window (the same as opening `baton-app.exe`) |
| `baton add <name> [--email <address>] [--priority N]` | create an account profile and sign in with Claude's own login (`--email` pre-fills the sign-in page) |
| `baton login <name> [--email <address>]` | sign an account in again (e.g. when `baton list` shows `needs_login`); refuses while a session runs on it |
| `baton list` | accounts, plan, and status (`ok`, `limited until …`, `needs_login`, `blocked`) |
| `baton enable` / `disable <name>` | include or skip an account (`enable` also clears a `blocked` mark) |
| `baton remove <name> [--force]` | log out and delete the profile (refuses if conversations started there) |
| `baton run [--account <name>] [-- <claude args>]` | start a supervised session |
| `baton switch [<name>] [--run <id>]` | move a running session to another account once its current turn ends (from another terminal) |
| `baton stop [--run <id>]` | end a running session the careful way; it can be continued later |
| `baton uninstall [--yes]` | sign every account out, delete baton's data and its Start menu entry |
| `baton status` | accounts, running sessions, recent switches |
| `baton sync` | share your normal Claude Code setup with every account profile (see below) |
| `baton config [<key> [<value>]]` | settings (below) |
| `baton doctor` | check claude.exe, the console and every login |

Settings (`baton config`): `switch_mode` (auto/confirm/off), `on_exhausted` (wait/stop:
what to do when every account is limited), `cooldown_seconds`, `max_switches_per_hour`,
`exhausted_retry_minutes` (used when the limit message has no reset time),
`continue_prompt`, `share_user_config` (default true), `claude_path` (full path to
`claude.exe`; `auto` goes back to detecting it), `allow_desktop_bundle`.

### Your Claude Code setup on every account

Each account is a separate Claude Code profile, so baton shares your normal setup
(`%USERPROFILE%\.claude`) with every profile before it starts Claude Code there:
`agents`, `commands`, `skills` and `output-styles` are linked (NTFS junctions), and
`settings.json`, `CLAUDE.md` and `keybindings.json` are copied. A file you edit inside a
profile is never overwritten, and login-related settings (`apiKeyHelper`, API-key or
provider environment variables) are left out of the copy so they cannot override the
account's own login. Plugins, MCP servers and credentials are not shared. Run
`baton sync` to refresh all profiles, or `baton config share_user_config false` to turn
this off.

### Resuming

`baton run -- -r <session id>` finds the session in whichever account's profile it is
stored, or in your normal Claude Code setup (`%USERPROFILE%\.claude`) for conversations
from before baton, so you can resume any conversation on any account; `baton run -- -c`
continues the most recent one in the current folder from any of those. When Claude Code exits,
baton prints a `baton run -- -r "<transcript>"` command that always works (Claude
Code's own `claude --resume <id>` hint only works on the account that started it).

Data lives in `%LOCALAPPDATA%\baton` (override with `BATON_HOME`):
`accounts.json`, `state.json`, `config.json`, `profiles\<name>\` (Claude Code's own
profile per account), `runs\<id>\` (hook events and a log per session) and `logs\gui.log`
(the window's own log).

## Things to know

- **Read this before turning switching on.** Only use accounts you personally own;
  sharing accounts between people is against Anthropic's terms. Anthropic's Usage Policy
  also prohibits intentionally bypassing restrictions in its products without permission
  and using multiple accounts to get around its safeguards. Continuing past a usage limit
  on another account may be treated as exactly that, and Anthropic can suspend accounts
  without notice. No Anthropic source says this use is allowed — you decide, at your own
  risk. baton shows this notice and asks before the first automatic switch. If an account
  is put on hold, has a billing problem or its login is revoked, baton does not switch away
  from it (see below).
- Account-changing commands (`baton switch`, `add`, `login`, `remove`, `sync`, `config …`)
  refuse to run from inside a supervised session. This makes it harder for the model in
  that session to move it, but it is not a security boundary (the session runs as you).
  In `confirm` mode every switch, including one asked for with `baton switch`, needs your
  answer in that session's own console window, not the baton app window (Enter means yes,
  Ctrl+C stops baton), and a running session
  only ever picks up a *stricter* switch mode (auto → confirm → off); loosening it takes
  effect at the next `baton run`.
- `baton switch` lets Claude Code finish the turn it is working on first: up to a minute
  (then baton interrupts it and the next account picks the task up again), or up to 30
  minutes in `confirm` mode, where you are asked anyway. `baton status` shows such a
  session as `stopping`; another `baton switch <name>` meanwhile changes the target.
- If an account is put on hold, has a billing problem or its login is revoked, baton does
  not switch away from it: the session stays on that account so you can see the error, and
  the account is marked `blocked` (baton says so when Claude Code exits). baton never uses a
  blocked account automatically; once the problem is resolved, `baton enable <name>` or a
  successful `baton login <name>` clears the mark.
- The first start on each account profile downloads Claude Code's official plugin
  marketplace (about 15 MB), as Claude Code does for any new profile.
- **Each switch costs quota on the new account**: prompt caches are per account, so the
  next account reads the whole conversation again. Long sessions benefit from `/compact`
  before they run out.
- **What does not follow the session**: background shells and MCP server state restart
  with Claude Code; up-arrow prompt history and `/rewind` checkpoints are per profile.
- Keys you type during the 5–10 s switch may be lost.
- When a login fails, Claude Code first retries on its own (up to 10 times), so baton
  moves on only after that; a 529 "overloaded" error never causes a switch.
- A conversation is stored in the profile of the account that started it. Removing that
  account deletes the conversation, so `baton remove` refuses unless `--force`.
- Only sessions started from baton's window or with `baton run` are covered (a session
  started from the window is a `baton run` in its own console) — not the Claude desktop
  app's own Code tab, and not IDE extensions.
- Run baton from Windows Terminal, PowerShell or cmd. Started from a terminal that is
  not a Windows console (for example mintty without ConPTY), baton opens Claude Code in a
  new console window.

## Uninstall

In the window: *Help → Uninstall baton*. Or from the command line:

```bash
baton uninstall
```

It signs every account out through Claude Code, deletes baton's data folder
(`%LOCALAPPDATA%\baton`: each account's profile, including conversations that started
there) and removes the Start menu entry, the entry in Windows' installed apps and the
install folder. End running sessions first. Your Claude accounts and your own Claude Code
setup are not touched. (Do not delete `%LOCALAPPDATA%\baton` by hand with a tool that
follows folder links while profiles exist: they link into `%USERPROFILE%\.claude`; baton's
own delete never follows them.)

## Development

```bash
go test ./...                                         # unit tests
go build -trimpath -o bin/baton.exe ./cmd/baton
python -I test/e2e/e2e_baton.py <empty dir>           # real claude.exe vs a local mock API
go build -tags batontest -o bin/baton-test.exe ./cmd/baton   # adds `gui --no-window --test-spawn` for UI tests
```

The end-to-end test drives the real `baton.exe` and `claude.exe` in a ConPTY against
the mock Messages API from `spikes/phase0/harness` (no real account involved): automatic
switch, manual switch, confirm mode, all accounts exhausted, transient throttling and
overload (no switch), login failure (switch), billing block (no switch), `/clear`,
resume by id across accounts, config sharing, Ctrl+C at the question, waiting for a turn
before a switch, and `baton stop` (also during the question and while a switch waits). The window's server has its own unit tests
(`internal/gui`); `baton gui --no-window --test-spawn` (test build) serves the page for a
browser, with sign-ins run hidden against a fake claude and sessions only recorded.
`spikes/phase0/RESULTS.md` records the verified Claude Code behaviour the design relies on.

## License

MIT — see [LICENSE](LICENSE). baton bundles the Go standard library and
`golang.org/x/sys` (BSD-3-Clause); their notices are in
[THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt). baton is not made by or affiliated with
Anthropic.

## 한국어 요약

여러 개의 **본인 소유** Claude 계정을 로그인해 두면, 쓰던 계정이 한도에 걸릴 때 같은 대화를 다음
계정으로 자동으로 이어서 실행합니다(약 5~10초). **baton-app.exe**(창)에서 계정을 추가하고 세션을 시작하면
되고, 같은 기능을 `baton.exe`로 명령줄에서도 쓸 수 있습니다(`baton add <이름>`, `baton run`). Claude 데스크톱
앱에 들어 있는 Claude Code를 그대로 쓰므로 따로 설치할 것이 없습니다. 계정마다 처음 한 번만 로그인하면(2단계
인증 포함) 로그인이 유지됩니다. 인증 정보는 Claude Code가 직접 관리하며 baton은
읽거나 저장하지 않습니다. 계정 공유는 Anthropic 약관 위반입니다. 또한 Anthropic 이용 정책은 허가 없이
제품의 제한을 의도적으로 우회하거나 여러 계정으로 안전장치를 피하는 것을 금지하므로, 한도에 걸린 뒤
다른 계정으로 이어 쓰는 것이 위반으로 판단될 수 있고 계정이 예고 없이 정지될 수 있습니다. 이를 허용한다는
Anthropic의 공식 입장은 없으니 본인 판단과 책임으로 사용하세요. 라이선스는 MIT입니다(LICENSE 참고).
