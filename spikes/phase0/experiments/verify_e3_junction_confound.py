"""verify_e3_junction_confound: adversarial follow-ups to e3_junction (does not modify e3 or the harness).

    python -I spikes/phase0/experiments/verify_e3_junction_confound.py <empty run_root>

Questions:
  Q1  Is the Read prompt for a session's own sidecar caused by the JUNCTION or by RESUME (-r)?
        G  junctioned profile, fresh --session-id, reads its own sidecar in the same process
        H  plain profile: reads own sidecar in-process, then quits, relaunches `-r SH`, reads it again
  Q2  Can a NARROWER rule replace e3's Read(//c/<profiles-root>/**)?  That broad rule also grants
      silent reads of every profile's own files (e.g. an OAuth .credentials.json). Here a dummy file
      <profiles>/G/secret-dummy.txt stands in for such a file.
        K  junctioned, --settings Read(//c/<profiles>/*/projects/**) + Read(//c/<store>/**)
        W  junctioned, --settings Read(//c/<profiles>/**) + Read(//c/<store>/**)   (e3 variant F)
  Q3  Stub sessions: does a launch with NO input write a .jsonl into the store? Is a /resume+Esc stub
      listed by a later /resume picker in another profile?
  Q4  (indicative only, mock classifier) junctioned profile in default AUTO mode reading a sidecar.
Prints one JSON report (also <run>/report.json).
"""
from __future__ import annotations

import glob
import json
import os
import re
import sys
import time
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(HERE), "harness"))
sys.path.insert(0, HERE)
from cc import Run  # noqa: E402
from conpty import strip_ansi  # noqa: E402
from mockapi import read_log, write_scenario  # noqa: E402
import e3_junction as e3  # noqa: E402  (helpers only; e3.main is not run)

BASE_ALLOW = ["Bash", "PowerShell", "Write", "Edit"]


def posix(p: str) -> str:
    return "//" + p[0].lower() + p[2:].replace("\\", "/")


def dbg_stats(path: str) -> dict:
    try:
        t = open(path, encoding="utf-8", errors="replace").read()
    except OSError:
        return {"missing": True}
    return {"permission_request_hooks_for_Read": t.count("executePermissionRequestHooks called for tool: Read"),
            "read_decision_ms": re.findall(r"tool_dispatch_start tool=Read \S+ permissionDecisionMs=(\d+)", t)}


def sidecar_of(body_log: str, letter: str) -> str | None:
    rows = [r for r in read_log(body_log) if (r.get("account") or "").endswith("k-" + letter) and r.get("main_loop")]
    return next((p for r in rows for p in r.get("tool_result_paths", [])
                 if os.sep + "profiles" + os.sep + letter + os.sep in p), None)


def probe(pc, scen, scen_base, key, letter, path, body_log, mock_log, label) -> dict:
    write_scenario(scen, {**scen_base, key: {"mode": "read_path", "path": path}})
    nb = len(read_log(body_log))
    nm = len(read_log(mock_log))
    time.sleep(0.5)
    m = pc.mark()
    done = e3.mock_done(mock_log, "k-" + letter, e3.READ_DONE)
    pc.submit(f"V3 {label}")
    out = {"label": label, "path": e3.mask(path)}
    out.update(e3.read_probe(pc, done, body_log, nb, m, label))
    out["dialog_tail"] = [d["dialog_text"][-160:] for d in out.pop("permission_dialogs")]
    side = [r for r in read_log(mock_log)[nm:] if r.get("method") == "POST" and "n" in r
            and (r.get("account") or "").endswith("k-" + letter) and not r.get("main_loop")]
    out["non_main_requests_during_probe"] = [{"n": r.get("n"), "user_text": (r.get("user_text") or "")[:120]}
                                             for r in side]
    return out


def picker_entries(text: str) -> list[str]:
    sp = " ".join(strip_ansi(text, " ").split())
    # the picker redraws; keep distinct "<label> <N> <unit> ago" entries
    ents = re.findall(r">?\s*([^·>│╰╭]{1,80}?)\s+(\d+\s*(?:seconds?|minutes?|hours?)\s*ago)", sp)
    return sorted({" ".join(a.split())[-60:] for a, _ in ents})


def main(root: str) -> int:
    run = Run(root)
    e3.MASK.extend([run.root, run.root.replace("\\", "\\\\"), run.root.replace("\\", "/"), posix(run.root)])
    rep: dict = {"id": "verify_e3_junction_confound", "claude": run.claude, "root": run.root}
    scen = os.path.join(run.root, "scenario.json")
    mock_log = os.path.join(run.logs, "mock.jsonl")
    body_log = os.path.join(run.logs, "bodies.jsonl")
    base = {"sk-mock-G": {"mode": "tool_big"}, "sk-mock-H": {"mode": "tool_big"}}
    write_scenario(scen, base)
    srv = e3.XMockServer(mock_log, scen, body_log)
    srv.start()
    store = os.path.join(run.root, "store", "projects")
    os.makedirs(store, exist_ok=True)
    profiles_root = os.path.join(run.root, "profiles")
    dbg = lambda n: os.path.join(run.logs, f"debug-{n}.txt")  # noqa: E731
    echo_all = {k: {"mode": "echo"} for k in ("sk-mock-G", "sk-mock-H", "sk-mock-K", "sk-mock-W", "sk-mock-P")}
    try:
        def mkprof(name, junc=True, extra=None):
            p = run.profile(name, f"sk-mock-{name}", srv.base_url, trust=[run.proj],
                            settings_extra=extra or {"permissions": {"allow": BASE_ALLOW}})
            if junc:
                e3.junction(os.path.join(p.dir, "projects"), store)
            return p

        g = mkprof("G")
        h = mkprof("H", junc=False)
        secret = os.path.join(g.dir, "secret-dummy.txt")
        with open(secret, "w", encoding="utf-8") as f:
            f.write("dummy-secret-line-1\ndummy-secret-line-2\n")

        # ---- Q1a: junctioned G, fresh session, own sidecar in-process -------------
        q1: dict = {}
        SG = str(uuid.uuid4())
        rep["SG"] = SG
        pc = run.pty(g, ["--permission-mode", "default", "--session-id", SG, "--debug-file", dbg("G_fresh")],
                     name="G_fresh")
        pc.start()
        pc.wait_for(e3.READY, 60)
        time.sleep(2)
        m = pc.mark()
        done = e3.mock_done(mock_log, "k-G", e3.TOOL_DONE)
        pc.submit("V3 G big tool")
        q1["G_tool_done"], dl = e3.drive(pc, done, 120, m, "G_tool")
        q1["G_tool_dialogs"] = len(dl)
        g_side = sidecar_of(body_log, "G")
        q1["G_sidecar_exists"] = bool(g_side) and os.path.isfile(g_side)
        q1["G_fresh_read_own_sidecar"] = probe(pc, scen, base, "sk-mock-G", "G", g_side, body_log, mock_log,
                                               "G_fresh_read_own") if g_side else "no sidecar"
        q1["G_quit"] = e3.quit_tui(pc)["exit_code"]
        q1["G_fresh_debug"] = dbg_stats(dbg("G_fresh"))

        # ---- Q1b: plain H, in-process read, then resume and read again ------------
        SH = str(uuid.uuid4())
        write_scenario(scen, {**echo_all, "sk-mock-H": {"mode": "tool_big"}})
        pc = run.pty(h, ["--permission-mode", "default", "--session-id", SH, "--debug-file", dbg("H_fresh")],
                     name="H_fresh")
        pc.start()
        pc.wait_for(e3.READY, 60)
        time.sleep(2)
        m = pc.mark()
        done = e3.mock_done(mock_log, "k-H", e3.TOOL_DONE)
        pc.submit("V3 H big tool")
        q1["H_tool_done"], _ = e3.drive(pc, done, 120, m, "H_tool")
        h_side = sidecar_of(body_log, "H")
        q1["H_projects_isjunction"] = os.path.isjunction(os.path.join(h.dir, "projects"))
        q1["H_fresh_read_own_sidecar"] = probe(pc, scen, echo_all, "sk-mock-H", "H", h_side, body_log, mock_log,
                                               "H_fresh_read_own") if h_side else "no sidecar"
        q1["H_quit1"] = e3.quit_tui(pc)["exit_code"]
        q1["H_fresh_debug"] = dbg_stats(dbg("H_fresh"))
        write_scenario(scen, echo_all)
        done = e3.mock_done(mock_log, "k-H", e3.ECHO_DONE)
        pc = run.pty(h, ["--permission-mode", "default", "-r", SH, "V3 H resumed", "--debug-file", dbg("H_resume")],
                     name="H_resume")
        pc.start()
        q1["H_resume_reply"], _ = e3.drive(pc, done, 90, 0, "H_resume", fail_rx=r"No conversation found")
        if h_side:
            q1["H_resumed_read_own_sidecar"] = probe(pc, scen, echo_all, "sk-mock-H", "H", h_side, body_log,
                                                     mock_log, "H_resumed_read_own")
        q1["H_quit2"] = e3.quit_tui(pc)["exit_code"]
        q1["H_resume_debug"] = dbg_stats(dbg("H_resume"))
        rep["Q1_junction_vs_resume"] = q1

        # ---- Q2: narrow vs broad Read rules (launcher --settings) -----------------
        q2: dict = {}
        rel = g_side.split(os.sep + "projects" + os.sep, 1)[1] if g_side else None
        variants = {
            "K_narrow": {"permissions": {"allow": [f"Read({posix(profiles_root)}/*/projects/**)",
                                                   f"Read({posix(store)}/**)"]}},
            "W_broad_as_e3_F": {"permissions": {"allow": [f"Read({posix(profiles_root)}/**)",
                                                          f"Read({posix(store)}/**)"]}},
        }
        for vname, flag in variants.items():
            letter = vname[0]
            pz = mkprof(letter)
            fpath = os.path.join(run.root, f"flag-{letter}.json")
            with open(fpath, "w", encoding="utf-8") as f:
                json.dump(flag, f, indent=1)
            mv: dict = {"flag_settings": json.loads(e3.mask(json.dumps(flag)))}
            write_scenario(scen, echo_all)
            done = e3.mock_done(mock_log, "k-" + letter, e3.ECHO_DONE)
            pc = run.pty(pz, ["--permission-mode", "default", "--settings", fpath, "-r", SG, f"V3 {vname} resume",
                              "--debug-file", dbg(vname)], name=vname)
            pc.start()
            mv["resume_reply"], _ = e3.drive(pc, done, 90, 0, vname, fail_rx=r"No conversation found")
            targets = []
            if vname.startswith("K") and g_side:
                targets += [("G_path_sidecar", g_side), ("own_path_sidecar", os.path.join(pz.dir, "projects", rel))]
            targets += [("other_profile_secret_dummy", secret)]
            mv["probes"] = [probe(pc, scen, echo_all, f"sk-mock-{letter}", letter, pth, body_log, mock_log,
                                  f"{vname}:{lab}") for lab, pth in targets]
            mv["quit"] = e3.quit_tui(pc)["exit_code"]
            mv["debug"] = dbg_stats(dbg(vname))
            q2[vname] = mv
        rep["Q2_rule_scope"] = q2

        # ---- Q3: stubs ----------------------------------------------------------------
        q3: dict = {}
        js = lambda: sorted(glob.glob(os.path.join(store, "*", "*.jsonl")))  # noqa: E731
        before = set(js())
        pc = run.pty(g, ["--debug-file", dbg("G_idle")], name="G_idle")
        pc.start()
        pc.wait_for(e3.READY, 60)
        time.sleep(4)
        q3["idle_quit"] = e3.quit_tui(pc)
        after_idle = set(js())
        q3["idle_launch_new_jsonl"] = [os.path.basename(p) for p in sorted(after_idle - before)]
        q3["idle_launch_new_jsonl_rows"] = {os.path.basename(p): [r.get("type") for r in e3.read_jsonl(p)]
                                            for p in sorted(after_idle - before)}
        pc = run.pty(g, ["--debug-file", dbg("G_resumecancel")], name="G_resumecancel")
        pc.start()
        pc.wait_for(e3.READY, 60)
        time.sleep(2)
        pc.send("/resume")
        time.sleep(1.2)
        pc.send("\r")
        time.sleep(4)
        pc.send("\x1b")
        time.sleep(1.2)
        q3["resumecancel_quit"] = e3.quit_tui(pc)["exit_code"]
        after_rc = set(js())
        q3["resumecancel_new_jsonl"] = [os.path.basename(p) for p in sorted(after_rc - after_idle)]
        q3["store_jsonl_now"] = {os.path.basename(p): [r.get("type") + ("/" + r["subtype"] if r.get("subtype") else "")
                                                       for r in e3.read_jsonl(p)][:12] for p in js()}
        # picker from another junctioned profile (K): which sessions are listed?
        kprof = run.profile("K", "sk-mock-K", srv.base_url, trust=[run.proj],
                            settings_extra={"permissions": {"allow": BASE_ALLOW}}, dir=os.path.join(profiles_root, "K"))
        pc = run.pty(kprof, ["--debug-file", dbg("K_picker")], name="K_picker")
        pc.start()
        pc.wait_for(e3.READY, 60)
        time.sleep(2)
        pc.send("/resume")
        time.sleep(1.2)
        m2 = pc.mark()
        pc.send("\r")
        pc.wait_for(r"ago", 15, since=m2)
        time.sleep(3)
        q3["picker_entries"] = picker_entries(pc.text[m2:])
        q3["picker_text_tail"] = " ".join(strip_ansi(pc.text[m2:], " ").split())[-900:]
        pc.send("\x1b")
        time.sleep(1.2)
        q3["picker_quit"] = e3.quit_tui(pc)["exit_code"]
        try:
            q3["K_picker_debug_resume_lines"] = [ln.strip()[25:220] for ln in open(dbg("K_picker"), encoding="utf-8",
                                                                                  errors="replace")
                                                 if "/resume" in ln]
        except OSError:
            pass
        rep["Q3_stubs"] = q3

        # ---- Q4: auto mode (indicative; mock answers the classifier with echo text) --
        q4: dict = {}
        pp = mkprof("P")
        write_scenario(scen, echo_all)
        done = e3.mock_done(mock_log, "k-P", e3.ECHO_DONE)
        pc = run.pty(pp, ["-r", SG, "V3 P auto resume", "--debug-file", dbg("P_auto")], name="P_auto")
        pc.start()
        q4["resume_reply"], q4["resume_dialogs"] = e3.drive(pc, done, 90, 0, "P_auto", fail_rx=r"No conversation found")
        q4["footer_auto_mode"] = bool(e3.seen(pc, r"auto mode on"))
        if g_side:
            q4["probe"] = probe(pc, scen, echo_all, "sk-mock-P", "P", os.path.join(pp.dir, "projects", rel),
                                body_log, mock_log, "P_auto:own_path_sidecar")
        q4["quit"] = e3.quit_tui(pc)["exit_code"]
        q4["debug"] = dbg_stats(dbg("P_auto"))
        try:
            t = open(dbg("P_auto"), encoding="utf-8", errors="replace").read()
            q4["debug_classifier_lines"] = [e3.mask(ln.strip())[25:260] for ln in t.splitlines()
                                            if re.search(r"classif|auto-mode|autoMode|yolo", ln, re.I)][-12:]
        except OSError:
            pass
        rep["Q4_auto_mode_indicative"] = q4
    finally:
        srv.stop()
    out = json.dumps(rep, ensure_ascii=False, indent=1)
    with open(os.path.join(run.root, "report.json"), "w", encoding="utf-8") as f:
        f.write(out)
    print(out)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
