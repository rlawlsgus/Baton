"""Hook command for e7_switchloop: append the hook's stdin payload + timestamp to an events file.

    <python> -I e7_switchloop_hook.py <event-name> <events-file>

Exits 0 quickly and prints nothing (so it never blocks or alters claude's behaviour).
"""
import json
import os
import sys
import time


def main() -> int:
    ts = time.time()
    event = sys.argv[1] if len(sys.argv) > 1 else "?"
    out = sys.argv[2] if len(sys.argv) > 2 else os.environ.get("E7_EVENTS_FILE", "")
    raw = ""
    try:
        raw = sys.stdin.buffer.read().decode("utf-8", "replace")
    except Exception as e:  # noqa: BLE001
        raw = f"<stdin read failed: {e!r}>"
    try:
        payload = json.loads(raw) if raw.strip() else None
    except ValueError:
        payload = None
    rec = {"ts": ts, "event": event, "pid": os.getpid(), "ppid": os.getppid(),
           "payload": payload, "raw": None if payload is not None else raw[:2000]}
    if out:
        line = json.dumps(rec, ensure_ascii=False) + "\n"
        # one write() call in append mode, so concurrent hook processes don't interleave lines
        fd = os.open(out, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o644)
        try:
            os.write(fd, line.encode("utf-8"))
        finally:
            os.close(fd)
    return 0


if __name__ == "__main__":
    sys.exit(main())
