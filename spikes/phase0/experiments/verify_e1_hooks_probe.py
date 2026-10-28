"""Adversarial follow-up probes for e1_hooks (does not modify e1_hooks.py or the harness).

    python -I verify_e1_hooks_probe.py <empty run_root>

T1  exec-form hook whose COMMAND (the exe path) contains a space + Hangul (junction to the Python
    install dir), script path with space + Hangul, project dir (cwd) with space + Hangul.
    echo -> 429 limit -> exit. Checks argv, parent, StopFailure, transcript path, CLAUDE_PROJECT_DIR.
T2  untrusted profile: trust dialog; press ONLY Enter (nothing typed). Exit code? Any hook fired?
T3  untrusted profile: trust dialog; type only "1" (no Enter). Does a digit select an option?
Prints one JSON report and writes report.json in the run root.
"""
from __future__ import annotations

import json
import os
import re
import shutil
import sys
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
from cc import Run, read_jsonl, transcripts, junction, wait_until  # noqa: E402
from mockapi import MockServer, write_scenario  # noqa: E402

HOOKLOG_SRC = os.path.join(HERE, "e1_hooks_hooklog.py")
READY_RX = r"for shortcuts|auto mode on|\? for|>\s"


def main(root: str) -> int:
    if os.path.exists(root) and os.listdir(root):
        print(json.dumps({"error": f"run_root not empty: {root}"}))
        return 2
    run = Run(root)
    R = run.root
    events = os.path.join(R, "events.jsonl")
    scen = os.path.join(R, "scenario.json")
    accounts = {"sk-mock-P": {"mode": "echo"}, "sk-mock-U": {"mode": "echo"}, "sk-mock-W": {"mode": "echo"}}
    write_scenario(scen, accounts)
    srv = MockServer(os.path.join(run.logs, "mock.jsonl"), scen)
    srv.start()
    rep: dict = {"run_root": R, "claude": run.claude}

    # space + Hangul everywhere
    script = os.path.join(R, "훅 스크립트 폴더", "hook 로거.py")
    os.makedirs(os.path.dirname(script), exist_ok=True)
    shutil.copyfile(HOOKLOG_SRC, script)
    pyj_dir = os.path.join(R, "파이썬 실행 폴더")
    junction(pyj_dir, os.path.dirname(sys.executable))
    PYJ = os.path.join(pyj_dir, os.path.basename(sys.executable))
    proj = os.path.join(R, "프로젝트 폴더 A")
    os.makedirs(proj, exist_ok=True)
    with open(os.path.join(proj, "README.md"), "w", encoding="utf-8") as f:
        f.write("probe\n")
    rep["paths"] = {"exe": PYJ, "script": script, "proj": proj, "exe_exists": os.path.isfile(PYJ)}

    def X(sess, tag, cmd=PYJ):
        return {"type": "command", "command": cmd, "timeout": 30,
                "args": ["-I", script, "--sess", sess, "--tag", tag, "--out", events,
                         "--probe", "sp ace", "한글 인자", "${CLAUDE_PROJECT_DIR}"]}

    def evs(sess):
        return [e for e in read_jsonl(events) if e.get("sess") == sess]

    def write_settings(name, obj):
        d = os.path.join(R, "런처 설정")
        os.makedirs(d, exist_ok=True)
        p = os.path.join(d, name)
        with open(p, "w", encoding="utf-8") as f:
            json.dump(obj, f, indent=1, ensure_ascii=False)
        return p

    # ---------------- T1 ----------------
    t1: dict = {}
    try:
        P = run.profile("P", "sk-mock-P", srv.base_url, trust=[proj])
        sp = write_settings("T1 설정.json", {"hooks": {
            "SessionStart": [{"hooks": [X("T1", "T1-SS")]}],
            "UserPromptSubmit": [{"hooks": [X("T1", "T1-UPS")]}],
            "Stop": [{"hooks": [X("T1", "T1-Stop")]}],
            "StopFailure": [{"matcher": "rate_limit", "hooks": [X("T1", "T1-SF")]}],
            "SessionEnd": [{"hooks": [X("T1", "T1-SE")]}]}})
        pc = run.pty(P, ["--settings", sp], name="T1", cwd=proj)
        pc.start()
        t1["ready"] = bool(pc.wait_for(READY_RX, 60))
        t1["ss_seen"] = bool(wait_until(lambda: [e for e in evs("T1") if e["tag"] == "T1-SS"], 15))
        time.sleep(1.5)
        m = pc.mark()
        pc.submit("t1 first prompt")
        t1["reply1"] = bool(pc.wait_for(r"MOCK-REPLY\[P\]", 60, since=m))
        wait_until(lambda: [e for e in evs("T1") if e["tag"] == "T1-Stop"], 15)
        time.sleep(1.5)
        accounts["sk-mock-P"] = {"mode": "limit"}
        write_scenario(scen, accounts)
        pc.submit("t1 second prompt")
        t1["sf_seen"] = bool(wait_until(lambda: [e for e in evs("T1") if e["tag"] == "T1-SF"], 60))
        time.sleep(2.0)
        pc.send("\x03"); time.sleep(0.6); pc.send("\x03")
        t1["exit_code"] = pc.wait_exit(20)
        wait_until(lambda: [e for e in evs("T1") if e["tag"] == "T1-SE"], 10)
        t1["close_code"] = pc.close()
        rows = []
        for e in evs("T1"):
            argv = e.get("argv") or []
            got = argv[argv.index("--probe") + 1:] if "--probe" in argv else None
            p = e.get("stdin") or {}
            rows.append({"tag": e["tag"], "event": p.get("hook_event_name"), "error": p.get("error"),
                         "parents": [x.get("exe") for x in (e.get("parents") or [])[:3]],
                         "parent_pid_is_claude": len(e.get("parents") or []) > 1 and e["parents"][1]["pid"] == pc.pid,
                         "argv0_exact": os.path.normcase(argv[0]) == os.path.normcase(script) if argv else None,
                         "probe": got, "raw_cmdline_head": (e.get("raw_cmdline") or "")[:200],
                         "sys_executable_hint": e.get("raw_cmdline", "").split(" -I ")[0],
                         "CLAUDE_PROJECT_DIR": (e.get("env") or {}).get("CLAUDE_PROJECT_DIR"),
                         "cwd": e.get("cwd"),
                         "transcript_path": p.get("transcript_path"),
                         "transcript_exists_at_hook": (e.get("transcript_at_hook") or {}).get("exists")})
        t1["events"] = rows
        t1["transcripts"] = [os.path.relpath(x, P.dir) for x in transcripts(P.dir)]
        t1["screen_tail"] = pc.plain()[-700:]
    except Exception as ex:  # noqa: BLE001
        t1["error"] = repr(ex)
    rep["T1_exe_path_space_hangul"] = t1

    # ---------------- T2 / T3: trust dialog ----------------
    for name, key, prof in (("T2", "\r", "U"), ("T3", "1", "W")):
        t: dict = {"key_sent": repr(key)}
        try:
            U = run.profile(prof, f"sk-mock-{prof}", srv.base_url, trust=None)
            sp = write_settings(f"{name}.json", {"hooks": {
                "SessionStart": [{"hooks": [X(name, f"{name}-SS")]}],
                "SessionEnd": [{"hooks": [X(name, f"{name}-SE")]}]}})
            pc = run.pty(U, ["--settings", sp], name=name)
            pc.start()
            got = wait_until(lambda: "Quicksafetycheck" in re.sub(r"\s+", "", pc.plain()), 30)
            t["dialog_seen"] = bool(got)
            t["dialog_secs"] = round(time.time() - pc.started_at, 1)
            time.sleep(1.5)
            sq = re.sub(r"\s+", "", pc.plain())
            t["pointer_before_key"] = ("No, exit" if ">No,exit" in sq else
                                       "Yes, I trust" if ">Yes,Itrust" in sq else None)
            t["events_before_key"] = [e["tag"] for e in evs(name)]
            m = pc.mark()
            pc.send(key)
            code = pc.wait_exit(8)
            t["exit_code_within_8s"] = code
            t["alive_after_8s"] = code is None
            t["raw_after_key"] = pc.text[m:][-400:]
            if code is None:
                pc.send("\x1b")  # Esc to cancel
                time.sleep(1.0)
                t["exit_code_after_esc"] = pc.wait_exit(8)
            t["close_code"] = pc.close()
            t["events_after"] = [e["tag"] for e in evs(name)]
            cj = U.read_claude_json()
            t["projects_after"] = {k: v.get("hasTrustDialogAccepted") for k, v in (cj.get("projects") or {}).items()}
        except Exception as ex:  # noqa: BLE001
            t["error"] = repr(ex)
        rep[f"{name}_trust_dialog"] = t

    srv.stop()
    txt = json.dumps(rep, ensure_ascii=False, indent=1, default=str)
    with open(os.path.join(R, "report.json"), "w", encoding="utf-8") as f:
        f.write(txt)
    print(txt)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
