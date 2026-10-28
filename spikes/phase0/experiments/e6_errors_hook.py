"""Exec hook used by e6_errors: append the hook's stdin JSON (plus a timestamp) to a JSONL file.

    python -I e6_errors_hook.py <out.jsonl> <label>
"""
import json
import os
import sys
import time


def main() -> int:
    out = sys.argv[1]
    label = sys.argv[2] if len(sys.argv) > 2 else ""
    raw = sys.stdin.buffer.read().decode("utf-8", "replace")
    try:
        payload = json.loads(raw) if raw.strip() else None
    except ValueError:
        payload = {"_unparsed": raw[:4000]}
    entry = {"ts": time.time(), "label": label, "pid": os.getpid(), "payload": payload}
    os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
    with open(out, "a", encoding="utf-8") as f:
        f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
