"""apiKeyHelper stand-in for e5_firstrun: logs each invocation, prints the dummy key.

    python -I e5_firstrun_keyhelper.py <log.jsonl> <dummy-key>
"""
import json
import os
import sys
import time

log, key = sys.argv[1], sys.argv[2]
with open(log, "a", encoding="utf-8") as f:
    f.write(json.dumps({"ts": time.time(), "key_suffix": key[-4:], "cwd": os.getcwd(),
                        "ppid": os.getppid()}) + "\n")
sys.stdout.write(key)
sys.stdout.flush()
