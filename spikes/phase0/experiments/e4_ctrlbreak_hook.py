"""e4_ctrlbreak exec hook logger: python -I e4_ctrlbreak_hook.py <out.jsonl> <EventName>

Appends {ts, event, pid, ppid, stdin JSON} to <out.jsonl>. Prints nothing.
"""
import json
import os
import sys
import time


def main() -> int:
    t = time.time()
    out, ev = sys.argv[1], sys.argv[2]
    try:
        raw = sys.stdin.buffer.read().decode("utf-8", "replace")
    except Exception as ex:  # noqa: BLE001
        raw = f"<stdin error {ex!r}>"
    try:
        payload = json.loads(raw) if raw.strip() else None
    except ValueError:
        payload = {"_raw": raw[:500]}
    with open(out, "a", encoding="utf-8") as f:
        f.write(json.dumps({"ts": t, "event": ev, "pid": os.getpid(), "ppid": os.getppid(),
                            "stdin": payload}, ensure_ascii=False) + "\n")
    if len(sys.argv) > 3:  # optional sleep, then a "<Event>:done" line (does claude wait for the hook?)
        time.sleep(float(sys.argv[3]))
        with open(out, "a", encoding="utf-8") as f:
            f.write(json.dumps({"ts": time.time(), "event": ev + ":done", "pid": os.getpid()}) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
