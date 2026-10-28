"""Helpers for running claude.exe in fully isolated test profiles.

Isolation rules (every experiment must keep them):
  * USERPROFILE/HOME point at a fake home inside the run dir, and every claude
    process gets CLAUDE_CONFIG_DIR inside the run dir, so the real ~/.claude and
    ~/.claude.json are never read or written.
  * The env is built from an allowlist; host-auth / token variables from the
    desktop app (CLAUDE_CODE_*, ANTHROPIC_*) are never inherited.
  * Auth is a dummy key from apiKeyHelper, and ANTHROPIC_BASE_URL points at the
    local mock, so nothing reaches Anthropic and no real account is used.
"""
from __future__ import annotations

import glob
import json
import os
import re
import shutil
import subprocess
import time
from dataclasses import dataclass, field

HERE = os.path.dirname(os.path.abspath(__file__))

ENV_ALLOW = [
    "SystemRoot", "SYSTEMROOT", "SystemDrive", "SYSTEMDRIVE", "windir", "WINDIR", "PATH", "Path", "PATHEXT",
    "COMSPEC", "ComSpec", "TEMP", "TMP", "APPDATA", "LOCALAPPDATA", "ProgramData", "PROGRAMDATA",
    "ProgramFiles", "PROGRAMFILES", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles",
    "USERNAME", "USERDOMAIN", "COMPUTERNAME", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE",
    "PROCESSOR_IDENTIFIER", "OS", "PSModulePath", "LANG",
]

QUIET_ENV = {
    "DISABLE_AUTOUPDATER": "1",
    "DISABLE_UPDATES": "1",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "DISABLE_TELEMETRY": "1",
    "DISABLE_ERROR_REPORTING": "1",
}


def find_claude() -> str:
    """claude.exe to test: $PHASE0_CLAUDE, native install, then newest desktop-bundled copy."""
    env = os.environ.get("PHASE0_CLAUDE")
    if env and os.path.isfile(env):
        return env
    native = os.path.join(os.path.expanduser("~"), ".local", "bin", "claude.exe")
    if os.path.isfile(native):
        return native
    root = os.path.join(os.environ.get("LOCALAPPDATA", ""), "Claude-Data")
    cands = glob.glob(os.path.join(root, "*", "claude-code", "*", "*", "claude.exe"))

    def ver(p: str) -> tuple:
        v = os.path.basename(os.path.dirname(os.path.dirname(p)))
        return tuple(int(x) for x in re.findall(r"\d+", v))

    if not cands:
        raise FileNotFoundError("no claude.exe found; set PHASE0_CLAUDE")
    return max(cands, key=ver)


def fwd(p: str) -> str:
    """Forward-slash form, as Claude Code keys projects in .claude.json."""
    return os.path.abspath(p).replace("\\", "/")


def short_path(p: str) -> str:
    import ctypes
    buf = ctypes.create_unicode_buffer(1024)
    n = ctypes.windll.kernel32.GetShortPathNameW(os.path.abspath(p), buf, 1024)
    return buf.value if n else os.path.abspath(p)


def long_path(p: str) -> str:
    """Expand 8.3 components (e.g. JOHNDO~1) so paths look like a real user's."""
    import ctypes
    buf = ctypes.create_unicode_buffer(1024)
    n = ctypes.windll.kernel32.GetLongPathNameW(os.path.abspath(p), buf, 1024)
    return buf.value if n else os.path.abspath(p)


def isolated_env(fake_home: str, config_dir: str | None = None, extra: dict | None = None) -> dict[str, str]:
    env = {k: os.environ[k] for k in ENV_ALLOW if k in os.environ}
    os.makedirs(fake_home, exist_ok=True)
    drive, rest = os.path.splitdrive(os.path.abspath(fake_home))
    env.update({"USERPROFILE": os.path.abspath(fake_home), "HOME": os.path.abspath(fake_home),
                "HOMEDRIVE": drive, "HOMEPATH": rest})
    env.update(QUIET_ENV)
    if config_dir:
        env["CLAUDE_CONFIG_DIR"] = os.path.abspath(config_dir)
    if extra:
        for k, v in extra.items():
            if v is None:
                env.pop(k, None)
            else:
                env[k] = v
    return env


@dataclass
class Profile:
    name: str
    dir: str
    api_key: str

    @property
    def claude_json(self) -> str:
        return os.path.join(self.dir, ".claude.json")

    @property
    def settings_json(self) -> str:
        return os.path.join(self.dir, "settings.json")

    def read_claude_json(self) -> dict:
        try:
            with open(self.claude_json, encoding="utf-8") as f:
                return json.load(f)
        except (OSError, ValueError):
            return {}

    def sessions(self) -> list[dict]:
        out = []
        for p in glob.glob(os.path.join(self.dir, "sessions", "*.json")):
            try:
                with open(p, encoding="utf-8") as f:
                    d = json.load(f)
                d["_file"] = p
                out.append(d)
            except (OSError, ValueError):
                pass
        return out


@dataclass
class Run:
    """One experiment's sandbox: <root>/{home, profiles/<name>, proj, logs}."""
    root: str
    claude: str = field(default_factory=find_claude)

    def __post_init__(self):
        os.makedirs(self.root, exist_ok=True)
        self.root = long_path(self.root)
        for d in ("home", "profiles", "proj", "logs"):
            os.makedirs(os.path.join(self.root, d), exist_ok=True)
        readme = os.path.join(self.proj, "README.md")
        if not os.path.exists(readme):
            with open(readme, "w", encoding="utf-8") as f:
                f.write("Phase 0 test project.\n")

    @property
    def home(self) -> str:
        return os.path.join(self.root, "home")

    @property
    def proj(self) -> str:
        return os.path.join(self.root, "proj")

    @property
    def logs(self) -> str:
        return os.path.join(self.root, "logs")

    def profile(self, name: str, api_key: str, base_url: str, *, onboarding: bool = True,
                trust: list[str] | None = None, settings_extra: dict | None = None,
                claude_json_extra: dict | None = None, dir: str | None = None) -> Profile:
        """Create (or rewrite the seed files of) a profile dir used as CLAUDE_CONFIG_DIR."""
        pdir = os.path.abspath(dir or os.path.join(self.root, "profiles", name))
        os.makedirs(pdir, exist_ok=True)
        settings = {
            "apiKeyHelper": f"echo {api_key}",
            "env": {"ANTHROPIC_BASE_URL": base_url},
            "permissions": {"allow": ["Bash", "PowerShell", "Read", "Write", "Edit"]},
        }
        if settings_extra:
            settings.update(settings_extra)
        with open(os.path.join(pdir, "settings.json"), "w", encoding="utf-8") as f:
            json.dump(settings, f, indent=1)
        cj: dict = {}
        if onboarding:
            cj.update({"hasCompletedOnboarding": True, "theme": "dark", "numStartups": 3,
                       "lastReleaseNotesSeen": "99.0.0", "hasSeenTasksHint": True})
        if trust:
            cj["projects"] = {fwd(p): {"hasTrustDialogAccepted": True, "allowedTools": []} for p in trust}
        if claude_json_extra:
            cj.update(claude_json_extra)
        with open(os.path.join(pdir, ".claude.json"), "w", encoding="utf-8") as f:
            json.dump(cj, f, indent=1)
        return Profile(name, pdir, api_key)

    def env(self, profile: Profile | None, extra: dict | None = None) -> dict[str, str]:
        return isolated_env(self.home, profile.dir if profile else None, extra)

    def run_print(self, profile: Profile, args: list[str], timeout: float = 120,
                  extra_env: dict | None = None, cwd: str | None = None) -> subprocess.CompletedProcess:
        """Run `claude <args>` non-interactively (pipes). Use for -p mode."""
        return subprocess.run([self.claude, *args], cwd=cwd or self.proj, env=self.env(profile, extra_env),
                              capture_output=True, text=True, encoding="utf-8", errors="replace",
                              timeout=timeout, stdin=subprocess.DEVNULL)

    def pty(self, profile: Profile, args: list[str], name: str = "pty", extra_env: dict | None = None,
            cwd: str | None = None, cols: int = 120, rows: int = 40):
        from conpty import PseudoConsole
        return PseudoConsole([self.claude, *args], cwd=cwd or self.proj, env=self.env(profile, extra_env),
                             cols=cols, rows=rows, log_dir=self.logs, name=name)


# ---- transcripts -----------------------------------------------------------

def transcripts(config_dir: str) -> list[str]:
    return sorted(glob.glob(os.path.join(config_dir, "projects", "*", "*.jsonl")))


def read_jsonl(path: str) -> list[dict]:
    rows = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    rows.append(json.loads(line))
                except ValueError:
                    rows.append({"_unparsed": line[:200]})
    except OSError:
        pass
    return rows


def session_ids(rows: list[dict]) -> list[str]:
    seen = []
    for r in rows:
        sid = r.get("sessionId")
        if sid and sid not in seen:
            seen.append(sid)
    return seen


def summarize_rows(rows: list[dict], limit: int = 50) -> list[dict]:
    """Compact view of transcript rows for reports."""
    out = []
    for r in rows[-limit:]:
        msg = r.get("message") or {}
        content = msg.get("content")
        kinds = []
        if isinstance(content, list):
            for b in content:
                if isinstance(b, dict):
                    t = b.get("type")
                    if t == "text":
                        kinds.append("text:" + (b.get("text") or "")[:60].replace("\n", " "))
                    else:
                        kinds.append(t)
        elif isinstance(content, str):
            kinds.append("str:" + content[:60].replace("\n", " "))
        out.append({k: r.get(k) for k in ("type", "subtype", "sessionId", "isApiErrorMessage", "error",
                                          "apiError", "isMeta", "interruptedByShutdown") if r.get(k) is not None}
                   | {"role": msg.get("role"), "content": kinds, "stop_reason": msg.get("stop_reason")})
    return out


def wait_until(pred, timeout: float = 30, interval: float = 0.25):
    deadline = time.time() + timeout
    while time.time() < deadline:
        v = pred()
        if v:
            return v
        time.sleep(interval)
    return None


def junction(link: str, target: str) -> None:
    """Create an NTFS junction (no admin needed)."""
    if os.path.lexists(link):
        raise FileExistsError(link)
    os.makedirs(target, exist_ok=True)
    r = subprocess.run(["cmd", "/c", "mklink", "/J", link, target], capture_output=True, text=True)
    if r.returncode != 0:
        raise OSError(f"mklink /J failed: {r.stdout} {r.stderr}")


def rmtree_safe(path: str) -> None:
    """Delete a run tree without following junctions into their targets."""
    for dirpath, dirnames, filenames in os.walk(path, topdown=True):
        for d in list(dirnames):
            full = os.path.join(dirpath, d)
            if os.path.isjunction(full) or os.path.islink(full):
                os.rmdir(full) if os.path.isjunction(full) else os.unlink(full)
                dirnames.remove(d)
    shutil.rmtree(path, ignore_errors=True)
