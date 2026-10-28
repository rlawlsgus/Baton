"""e5_firstrun: first-run dialogs of a brand-new profile and the .claude.json keys that suppress them.

    python -I spikes/phase0/experiments/e5_firstrun.py <empty run_root> [step ...]

Steps (default: all):
  s1   P0: profile with NO .claude.json, plain TUI launch; walk every dialog until the input
       prompt answers a probe; record dialog texts, keys, .claude.json evolution, helper/mock timing
  s3   P1: .claude.json = {"hasCompletedOnboarding": true}
  s4   P2 variants: onboarding + projects[<key variant>].hasTrustDialogAccepted
  s5   untrusted profile: TUI `-r <transcript path> "<prompt>"` (transcript made in a trusted profile);
       also a fully fresh profile and a trusted control
  s6   -p in an untrusted cwd (and with no .claude.json at all)
(s2 = diff of P0's .claude.json and s7 = helper/mock-before-trust are derived from s1/s5.)

Every claude process goes through cc.Run (fake HOME, per-profile CLAUDE_CONFIG_DIR, dummy key
via apiKeyHelper, mock base URL). apiKeyHelper is e5_firstrun_keyhelper.py, which logs each call.
"""
from __future__ import annotations

import json
import os
import re
import sys
import threading
import time
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
sys.path.insert(0, HERE)
from cc import Run, Profile, fwd, short_path, transcripts, read_jsonl, summarize_rows  # noqa: E402
from mockapi import MockServer, read_log, write_scenario  # noqa: E402
from e5_firstrun_vt import Screen  # noqa: E402

KEYHELPER = os.path.join(HERE, "e5_firstrun_keyhelper.py")

# Input box of the main REPL screen: a rule line, a line starting with '>', a rule line.
READY_RX = re.compile(r"^─{20,}\n>.*\n─{20,}", re.M)

# Known dialogs: (name, regex on the current screen, keys that answer it, note).
# Order matters: first match wins.
RULES: list[tuple[str, str, str, str]] = [
    ("theme", r"Choose the text style|text style that looks best|Dark mode", "\r", "Enter = default"),
    ("login_method", r"Select login method", "", "STOP: would need a real login"),
    ("api_key", r"Detected a custom API key|use this API key", "", "STOP: record"),
    ("security_notes", r"Security notes", "\r", "Enter = continue"),
    ("terminal_setup", r"terminal setup", "\r", "Enter = default"),
    ("trust", r"trust the files|trust this folder|Do you trust|Yes, I trust|Yes, proceed", "TRUST_YES",
     "move the highlight to 'Yes, I trust this folder', then Enter"),
]


def trust_yes_keys(screen: str) -> list[str]:
    """Keys that select the 'Yes' option of the trust dialog given which line is highlighted."""
    opts = [l.strip() for l in screen.splitlines() if re.match(r"^\s*(>\s+)?(No, exit|Yes, I trust|Yes, proceed)", l)]
    cur = next((i for i, l in enumerate(opts) if l.startswith(">")), 0)
    yes = next((i for i, l in enumerate(opts) if "Yes" in l), 1)
    moves = yes - cur
    return (["\x1b[B"] * moves if moves > 0 else ["\x1b[A"] * (-moves)) + ["\r"]

SECRETISH = re.compile(r"key|token|oauth|secret|password|credential", re.I)
IDENTIFIER_KEYS = {"userID", "machineID", "anonymousId"}


def now() -> float:
    return time.time()


# ---------------------------------------------------------------------------
# .claude.json watcher

def _summ_cj(d: dict) -> dict:
    """Key names + non-secret values (identifiers / secret-ish keys reduced to their type)."""
    out = {}
    for k, v in d.items():
        if k == "projects" and isinstance(v, dict):
            out[k] = {pk: sorted(pv.keys()) if isinstance(pv, dict) else type(pv).__name__
                      for pk, pv in v.items()}
        elif k in IDENTIFIER_KEYS or SECRETISH.search(k):
            out[k] = f"<{type(v).__name__} redacted>"
        elif isinstance(v, (bool, int, float)) or v is None:
            out[k] = v
        elif isinstance(v, str):
            out[k] = v if len(v) <= 60 else v[:57] + "..."
        else:
            s = json.dumps(v, ensure_ascii=False)
            out[k] = v if len(s) <= 120 else f"<{type(v).__name__} len={len(s)}>"
    return out


class CJWatcher:
    """Polls a profile's .claude.json; records each distinct content with a timestamp."""

    def __init__(self, path: str):
        self.path = path
        self.events: list[dict] = []
        self._stop = threading.Event()
        self._last: bytes | None = b"<init>"
        self.t = threading.Thread(target=self._loop, daemon=True)

    def start(self):
        self._poll()
        self.t.start()
        return self

    def _poll(self):
        try:
            with open(self.path, "rb") as f:
                data = f.read()
        except OSError:
            data = None
        if data != self._last:
            self._last = data
            if data is None:
                self.events.append({"ts": now(), "exists": False})
            else:
                try:
                    d = json.loads(data.decode("utf-8"))
                    self.events.append({"ts": now(), "exists": True, "keys": sorted(d.keys()),
                                        "summary": _summ_cj(d)})
                except ValueError:
                    self.events.append({"ts": now(), "exists": True, "unparsable_len": len(data)})

    def _loop(self):
        while not self._stop.is_set():
            self._poll()
            time.sleep(0.1)

    def stop(self):
        self._stop.set()
        self.t.join(2)
        self._poll()


# ---------------------------------------------------------------------------
# TUI driver with a screen model

class Tui:
    def __init__(self, run: Run, profile: Profile, args: list[str], name: str, cwd: str | None = None):
        self.run, self.profile, self.args, self.name = run, profile, args, name
        self.pc = run.pty(profile, args, name=name, cwd=cwd)
        self.scr = Screen(self.pc.cols, self.pc.rows)
        self._fed = 0
        self.snapshots: list[dict] = []

    def start(self):
        self.pc.start()
        self.t0 = self.pc.started_at
        return self

    def screen(self) -> str:
        raw = self.pc.raw
        if len(raw) > self._fed:
            self.scr.feed(raw[self._fed:])
            self._fed = len(raw)
        return self.scr.text()

    def settle(self, quiet: float = 0.8, max_wait: float = 6.0) -> None:
        end = now() + max_wait
        last, since = len(self.pc.raw), now()
        while now() < end:
            time.sleep(0.15)
            cur = len(self.pc.raw)
            if cur != last:
                last, since = cur, now()
            elif now() - since >= quiet:
                return
            if not self.pc.is_alive():
                return

    def snap(self, label: str) -> str:
        s = self.screen()
        self.snapshots.append({"t": round(now() - self.t0, 2), "label": label, "screen": s})
        return s

    def save_snapshots(self):
        p = os.path.join(self.run.logs, f"{self.name}.screens.txt")
        with open(p, "w", encoding="utf-8") as f:
            for s in self.snapshots:
                f.write(f"===== t={s['t']}s  {s['label']} =====\n{s['screen']}\n\n")

    def quit(self) -> int | None:
        if self.pc.is_alive():
            self.pc.send("\x03")
            time.sleep(0.6)
            self.pc.send("\x03")
            code = self.pc.wait_exit(15)
        else:
            code = self.pc.exit_code()
        self.pc.close()
        self.save_snapshots()
        return code


def send_keys(tui: "Tui", keys) -> None:
    """keys: a string, or a list of strings sent 0.35 s apart."""
    if isinstance(keys, str):
        tui.pc.send(keys)
        return
    for i, k in enumerate(keys):
        if i:
            time.sleep(0.35)
        tui.pc.send(k)


def match_rule(screen: str):
    for name, rx, keys, note in RULES:
        if re.search(rx, screen, re.I):
            return name, keys, note
    return None


def walk(tui: Tui, timeout: float = 90, explore: bool = True, answer_delay: dict | None = None,
         on_dialog=None, keys_override: dict | None = None) -> dict:
    """Answer dialogs until the REPL input box is on screen. Returns a record of what happened."""
    answer_delay = answer_delay or {}
    dialogs: list[dict] = []
    deadline = now() + timeout
    outcome = "timeout"
    repeat = 0
    blank_since = 0.0
    blanks: list[dict] = []
    while now() < deadline:
        if not tui.pc.is_alive():
            outcome = f"exited rc={tui.pc.exit_code()}"
            break
        tui.settle()
        scr = tui.screen()
        hit = match_rule(scr)
        if hit is None and READY_RX.search(scr):
            if blank_since:
                blanks.append({"from": round(blank_since - tui.t0, 2), "to": round(now() - tui.t0, 2)})
            tui.snap("ready")
            outcome = "ready"
            break
        if hit is None and not scr.strip():
            # blank transitional screen (e.g. between the trust dialog and the REPL): wait, send nothing
            if not blank_since:
                blank_since = now()
                tui.snap("blank")
            if now() - blank_since < 20:
                time.sleep(0.3)
                continue
        else:
            if blank_since:
                blanks.append({"from": round(blank_since - tui.t0, 2), "to": round(now() - tui.t0, 2)})
            blank_since = 0.0
        if hit is None:
            if not explore:
                tui.snap("unknown")
                outcome = "unknown_screen"
                dialogs.append({"name": "unknown", "t_seen": round(now() - tui.t0, 2), "screen": scr})
                break
            hit = (f"unknown{len(dialogs)}", "\r", "explore: Enter")
        name, keys, note = hit
        if keys_override and name in keys_override:
            keys, note = keys_override[name], "override"
        if keys == "TRUST_YES":
            keys = trust_yes_keys(scr)
        if dialogs and dialogs[-1]["name"] == name and dialogs[-1]["screen"] == scr:
            repeat += 1
            if repeat >= 3:
                outcome = f"stuck_on_{name}"
                break
        else:
            repeat = 0
        t_seen = now()
        tui.snap(f"dialog:{name}")
        rec = {"name": name, "t_seen": round(t_seen - tui.t0, 2), "ts_seen": t_seen, "screen": scr,
               "keys": keys, "note": note}
        if on_dialog:
            on_dialog(name, rec)
        if not keys:
            dialogs.append(rec)
            outcome = f"stopped_at_{name}"
            break
        d = answer_delay.get(name, 0)
        if d:
            time.sleep(d)
        rec["ts_answered"] = now()
        rec["t_answered"] = round(rec["ts_answered"] - tui.t0, 2)
        send_keys(tui, keys)
        dialogs.append(rec)
        # wait for the screen to change
        end = now() + 6
        while now() < end and tui.screen() == scr and tui.pc.is_alive():
            time.sleep(0.2)
    return {"outcome": outcome, "t_ready": round(now() - tui.t0, 2), "dialogs": dialogs, "blank_screens": blanks}


# ---------------------------------------------------------------------------

class Exp:
    def __init__(self, root: str):
        self.run = Run(root)
        self.scen = os.path.join(self.run.root, "scenario.json")
        write_scenario(self.scen, {})
        self.mock_log = os.path.join(self.run.logs, "mock.jsonl")
        self.srv = MockServer(self.mock_log, self.scen)
        self.srv.start()
        self.report: dict = {"claude": self.run.claude, "run_root": self.run.root, "proj": self.run.proj}

    def helper_log(self, name: str) -> str:
        return os.path.join(self.run.logs, f"helper_{name}.jsonl")

    def profile(self, name: str, claude_json: dict | None, cj_raw: str | None = None) -> Profile:
        """claude_json None => no .claude.json file at all."""
        key = f"sk-mock-e5{name.lower()[-2:]:>02}"
        helper = f'"{sys.executable}" -I "{KEYHELPER}" "{self.helper_log(name)}" {key}'
        p = self.run.profile(name, key, self.srv.base_url, onboarding=False,
                             settings_extra={"apiKeyHelper": helper})
        if claude_json is None:
            os.remove(p.claude_json)
        else:
            with open(p.claude_json, "w", encoding="utf-8") as f:
                f.write(cj_raw if cj_raw is not None else json.dumps(claude_json, indent=1))
        return p

    def helper_calls(self, name: str) -> list[float]:
        try:
            with open(self.helper_log(name), encoding="utf-8") as f:
                return [json.loads(l)["ts"] for l in f if l.strip()]
        except OSError:
            return []

    def mock_rows(self, p: Profile, t_from: float, t_to: float | None = None) -> list[dict]:
        suf = "..." + p.api_key[-4:]
        rows = [r for r in read_log(self.mock_log)
                if r.get("account") == suf and r.get("ts", 0) >= t_from and (t_to is None or r["ts"] <= t_to)]
        return rows

    @staticmethod
    def brief_mock(rows: list[dict], t0: float) -> list[dict]:
        return [{"t": round(r["ts"] - t0, 2), "path": r.get("path"), "status": r.get("status"),
                 "main_loop": r.get("main_loop"), "n_messages": r.get("n_messages"),
                 "user_text": r.get("user_text"), "reply": (r.get("text") or "")[:80]} for r in rows]

    def cj_timeline(self, w: CJWatcher, t0: float) -> list[dict]:
        out, prev = [], None
        for e in w.events:
            keys = e.get("keys")
            item = {"t": round(e["ts"] - t0, 2), "exists": e["exists"]}
            if keys is not None:
                item["added_keys"] = sorted(set(keys) - set(prev or []))
                if prev is not None and set(prev) - set(keys):
                    item["removed_keys"] = sorted(set(prev) - set(keys))
                s = e["summary"]
                item["projects"] = s.get("projects")
                item["hasCompletedOnboarding"] = s.get("hasCompletedOnboarding")
                prev = keys
            out.append(item)
        return out

    def tui_case(self, name: str, p: Profile, args: list[str], *, probe: str | None = None,
                 expect_auto: str | None = None, answer_delay: dict | None = None,
                 explore: bool = True, cwd: str | None = None, timeout: float = 90,
                 keys_override: dict | None = None) -> dict:
        w = CJWatcher(p.claude_json).start()
        tui = Tui(self.run, p, args, name, cwd=cwd).start()
        t0 = tui.t0
        res = walk(tui, timeout=timeout, explore=explore, answer_delay=answer_delay, keys_override=keys_override)
        out: dict = {"args": [a if not a.endswith(".jsonl") else "<transcript.jsonl>" for a in args],
                     "cwd": cwd or self.run.proj, **res}
        # helper calls / mock requests before each dialog was answered
        hc = self.helper_calls(p.name)
        for d in out["dialogs"]:
            ta = d.get("ts_answered", d["ts_seen"])
            d["helper_calls_before_answer"] = len([x for x in hc if t0 <= x <= ta])
            d["mock_requests_before_answer"] = len(self.mock_rows(p, t0, ta))
            d["mock_requests_any_account_before_answer"] = len(
                [r for r in read_log(self.mock_log) if t0 <= r.get("ts", 0) <= ta])
            d.pop("ts_seen", None)
            d.pop("ts_answered", None)
        if res["outcome"] == "ready":
            scr = tui.screen()
            out["auto_mode_banner_on_ready_screen"] = "Auto mode is now" in scr
            if expect_auto:
                got = None
                end = now() + 30
                while now() < end and not got:
                    got = [r for r in self.mock_rows(p, t0) if expect_auto in (r.get("user_text") or "")]
                    time.sleep(0.3)
                out["auto_submitted"] = bool(got)
                if got:
                    out["auto_submit_request"] = self.brief_mock(got[:1], t0)[0]
                out["reply_on_screen"] = bool(tui.pc.wait_for(r"MOCK-REPLY\[", 20))
                time.sleep(1)
                tui.snap("after_auto_prompt")
            if probe:
                time.sleep(1.5)
                m = tui.pc.mark()
                tsub = now()
                tui.pc.submit(probe)
                got = tui.pc.wait_for(re.escape(f"]") + r" #\d+: " + re.escape(probe[:20]), 45, since=m)
                out["probe_reply_on_screen"] = bool(got)
                rows = [r for r in self.mock_rows(p, tsub) if probe in (r.get("user_text") or "")]
                out["probe_mock_requests"] = len(rows)
                time.sleep(0.8)
                tui.snap("after_probe")
        out["exit_code"] = tui.quit()
        w.stop()
        t_end = now()
        out["helper_calls"] = [round(x - t0, 2) for x in self.helper_calls(p.name) if t0 <= x <= t_end]
        out["mock"] = self.brief_mock(self.mock_rows(p, t0, t_end), t0)
        out["claude_json_timeline"] = self.cj_timeline(w, t0)
        out["claude_json_final"] = _summ_cj(p.read_claude_json())
        out["screens_file"] = os.path.join(self.run.logs, f"{name}.screens.txt")
        return out

    # ---- steps ---------------------------------------------------------
    def s1(self):
        p = self.profile("P0", None)
        r = self.tui_case("s1_P0_fresh", p, [], probe="e5 probe from P0")
        r["profile_dir_listing"] = sorted(os.listdir(p.dir))
        self.report["s1_P0_fresh"] = r
        # s2: what CC wrote into a fresh .claude.json
        cj = p.read_claude_json()
        self.report["s2_P0_claude_json"] = {
            "keys": sorted(cj.keys()),
            "values": _summ_cj(cj),
            "projects_keys": list((cj.get("projects") or {}).keys()),
            "proj_fwd": fwd(self.run.proj),
        }

    def s3(self):
        p = self.profile("P1", {"hasCompletedOnboarding": True})
        self.report["s3_P1_onboarding_only"] = self.tui_case("s3_P1_onb", p, [], probe="e5 probe from P1")
        # s3b: what plain Enter does on the trust dialog
        pb = self.profile("P1b", {"hasCompletedOnboarding": True})
        r = self.tui_case("s3b_trust_plain_enter", pb, [], keys_override={"trust": "\r"}, timeout=40)
        self.report["s3b_trust_plain_enter"] = {
            "outcome": r["outcome"], "exit_code": r["exit_code"], "dialogs": [d["name"] for d in r["dialogs"]],
            "trust_screen": next((d["screen"] for d in r["dialogs"] if d["name"] == "trust"), None),
            "projects_after": r["claude_json_final"].get("projects"), "mock_requests": len(r["mock"]),
            "screens_file": r["screens_file"]}
        # s3c: what Esc does on the trust dialog
        pc_ = self.profile("P1c", {"hasCompletedOnboarding": True})
        r = self.tui_case("s3c_trust_esc", pc_, [], keys_override={"trust": "\x1b"}, timeout=40)
        self.report["s3c_trust_esc"] = {
            "outcome": r["outcome"], "exit_code": r["exit_code"], "dialogs": [d["name"] for d in r["dialogs"]],
            "projects_after": r["claude_json_final"].get("projects"), "mock_requests": len(r["mock"]),
            "screens_file": r["screens_file"]}

    def s4(self):
        proj = self.run.proj
        f = fwd(proj)
        variants = {
            "fwd_C": (f, None),
            "back_C": (proj, None),
            "fwd_lower_drive": (f[0].lower() + f[1:], None),
            "back_lower_drive": (proj[0].lower() + proj[1:], None),
            "fwd_trailing_slash": (f + "/", None),
            "fwd_all_lowercase": (f.lower(), None),
            "fwd_short83": (fwd(short_path(proj)), None),
            "parent_dir_fwd": (fwd(self.run.root), None),
            "fwd_C_but_cwd_lower_drive": (f, proj[0].lower() + proj[1:]),
            "grandparent_dir_fwd": (fwd(os.path.dirname(self.run.root)), None),
            "drive_root_fwd": (fwd(os.path.splitdrive(proj)[0] + "\\"), None),
            "drive_root_no_slash": (os.path.splitdrive(proj)[0], None),
            "control_no_projects": (None, None),
        }
        out = {}
        for i, (vname, (key, cwd)) in enumerate(variants.items()):
            cj: dict = {"hasCompletedOnboarding": True}
            if key is not None:
                cj["projects"] = {key: {"hasTrustDialogAccepted": True}}
            p = self.profile(f"V{i:02d}", cj)
            r = self.tui_case(f"s4_{vname}", p, [], explore=False, cwd=cwd, timeout=60)
            names = [d["name"] for d in r["dialogs"]]
            out[vname] = {
                "seed_key": key, "cwd": cwd or proj, "outcome": r["outcome"],
                "dialogs": names, "trust_dialog_shown": "trust" in names,
                "projects_after": (r["claude_json_final"].get("projects")),
                "mock_requests": len(r["mock"]), "screens_file": r["screens_file"],
            }
            if r["outcome"] not in ("ready",) and "trust" not in names:
                out[vname]["detail"] = r
        self.report["s4_trust_key_variants"] = out

    def _seed_transcript(self) -> tuple[str, str]:
        t = self.profile("T", {"hasCompletedOnboarding": True,
                               "projects": {fwd(self.run.proj): {"hasTrustDialogAccepted": True}}})
        sid = str(uuid.uuid4())
        cp = self.run.run_print(t, ["-p", "e5 seed message", "--session-id", sid, "--output-format", "json"],
                                timeout=120)
        tps = [x for x in transcripts(t.dir) if sid in os.path.basename(x)]
        self.report["s5_seed"] = {"rc": cp.returncode, "stdout_tail": cp.stdout[-300:],
                                  "stderr_tail": cp.stderr[-300:], "transcript": tps,
                                  "rows": summarize_rows(read_jsonl(tps[0]), 10) if tps else None}
        return sid, tps[0] if tps else ""

    def s5(self):
        sid, path = self._seed_transcript()
        if not path:
            self.report["s5_error"] = "seed transcript missing"
            return
        prompt = "e5 auto prompt after trust"
        # a) onboarded but untrusted profile; hold the trust dialog 8 s to see what runs before it
        u = self.profile("U1", {"hasCompletedOnboarding": True})
        self.report["s5a_untrusted_resume"] = self.tui_case(
            "s5a_untrusted_resume", u, ["-r", path, prompt], expect_auto=prompt,
            answer_delay={"trust": 8})
        # b) completely fresh profile (no .claude.json)
        u2 = self.profile("U2", None)
        self.report["s5b_fresh_resume"] = self.tui_case(
            "s5b_fresh_resume", u2, ["-r", path, prompt + " fresh"], expect_auto=prompt + " fresh",
            answer_delay={"trust": 4})
        # c) control: onboarded + trusted
        u3 = self.profile("U3", {"hasCompletedOnboarding": True,
                                 "projects": {fwd(self.run.proj): {"hasTrustDialogAccepted": True}}})
        self.report["s5c_trusted_resume_control"] = self.tui_case(
            "s5c_trusted_resume", u3, ["-r", path, prompt + " ctl"], expect_auto=prompt + " ctl")
        # d/e) the launcher's real relaunch shape: --settings <hook file> -r <path> "<prompt>"
        hooks = os.path.join(self.run.root, "e5_hooks.json")
        marker = fwd(os.path.join(self.run.logs, "stopfailure_hook.txt"))
        with open(hooks, "w", encoding="utf-8") as f:
            json.dump({"hooks": {"StopFailure": [{"hooks": [
                {"type": "command", "command": f"echo e5-stopfailure >> {marker}"}]}]}}, f, indent=1)
        u4 = self.profile("U4", {"hasCompletedOnboarding": True,
                                 "projects": {fwd(self.run.proj): {"hasTrustDialogAccepted": True}}})
        self.report["s5d_trusted_settings_resume"] = self.tui_case(
            "s5d_trusted_settings_resume", u4, ["--settings", hooks, "-r", path, prompt + " set"],
            expect_auto=prompt + " set")
        u5 = self.profile("U5", {"hasCompletedOnboarding": True})
        self.report["s5e_untrusted_settings_resume"] = self.tui_case(
            "s5e_untrusted_settings_resume", u5, ["--settings", hooks, "-r", path, prompt + " uset"],
            expect_auto=prompt + " uset", answer_delay={"trust": 2})
        for k, pr in (("s5a_untrusted_resume", u), ("s5b_fresh_resume", u2), ("s5c_trusted_resume_control", u3),
                      ("s5d_trusted_settings_resume", u4), ("s5e_untrusted_settings_resume", u5)):
            self.report[k]["transcripts_in_profile"] = [os.path.relpath(x, pr.dir) for x in transcripts(pr.dir)]
        self.report["s5_seed_transcript_rows_after"] = summarize_rows(read_jsonl(path), 12)

    def s6(self):
        out = {}
        for name, cj in (("W1", {"hasCompletedOnboarding": True}), ("W0", None)):
            p = self.profile(name, cj)
            t0 = now()
            cp = self.run.run_print(p, ["-p", f"e5 print untrusted {name}", "--output-format", "json"],
                                    timeout=120)
            t1 = now()
            res = None
            try:
                res = json.loads(cp.stdout.strip().splitlines()[-1]).get("result")
            except (ValueError, IndexError, AttributeError):
                pass
            out["no_claude_json" if cj is None else "onboarding_only"] = {
                "rc": cp.returncode, "secs": round(t1 - t0, 1), "result": res,
                "stderr_tail": cp.stderr[-400:],
                "helper_calls": len([x for x in self.helper_calls(name) if t0 <= x <= t1]),
                "mock": self.brief_mock(self.mock_rows(p, t0, t1), t0),
                "claude_json_after": _summ_cj(p.read_claude_json()),
            }
        self.report["s6_print_untrusted"] = out

    def summarize(self):
        r = self.report
        s: dict = {}

        def dl(case):
            return [d["name"] for d in (r.get(case) or {}).get("dialogs", [])]

        for case in ("s1_P0_fresh", "s3_P1_onboarding_only", "s5a_untrusted_resume", "s5b_fresh_resume",
                     "s5c_trusted_resume_control", "s5d_trusted_settings_resume", "s5e_untrusted_settings_resume"):
            c = r.get(case)
            if not c:
                continue
            trust = next((d for d in c.get("dialogs", []) if d["name"] == "trust"), None)
            s[case] = {"dialogs": dl(case), "outcome": c.get("outcome"),
                       "probe_reply": c.get("probe_reply_on_screen"), "auto_submitted": c.get("auto_submitted"),
                       "auto_submit_t": (c.get("auto_submit_request") or {}).get("t"),
                       "auto_submit_n_messages": (c.get("auto_submit_request") or {}).get("n_messages"),
                       "trust_answered_t": trust.get("t_answered") if trust else None,
                       "helper_call_t": c.get("helper_calls"),
                       "mock_before_trust_answer": trust.get("mock_requests_any_account_before_answer") if trust else None,
                       "blank_screens": c.get("blank_screens")}
        v = r.get("s4_trust_key_variants") or {}
        s["s4_honored"] = {k: (not x["trust_dialog_shown"]) for k, x in v.items()}
        p6 = r.get("s6_print_untrusted") or {}
        s["s6"] = {k: {"rc": x["rc"], "result": x["result"],
                       "projects_written": x["claude_json_after"].get("projects")} for k, x in p6.items()}
        r["summary"] = s

    def close(self):
        self.srv.stop()


def main(argv: list[str]) -> int:
    root = argv[1]
    steps = argv[2:] or ["s1", "s3", "s4", "s5", "s6"]
    e = Exp(root)
    try:
        for s in steps:
            t = now()
            try:
                getattr(e, s)()
            except Exception as ex:  # keep going, record
                import traceback
                e.report[f"{s}_exception"] = traceback.format_exc()[-2000:]
            e.report.setdefault("step_secs", {})[s] = round(now() - t, 1)
    finally:
        e.close()
    try:
        e.summarize()
    except Exception:
        import traceback
        e.report["summary_exception"] = traceback.format_exc()[-1500:]
    print(json.dumps(e.report, ensure_ascii=False, indent=1))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
