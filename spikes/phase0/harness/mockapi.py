"""Mock Anthropic Messages API for Phase 0 spikes.

No real account is involved: Claude Code is pointed at this server with
ANTHROPIC_BASE_URL=http://127.0.0.1:<port> and a dummy key from apiKeyHelper.
Each dummy key acts as one "account"; its behaviour comes from a scenario file
that is re-read on every request, so a test can change it while claude runs.

Scenario file (JSON):
{
  "default":  {"mode": "echo"},
  "accounts": {
    "sk-mock-A": {"mode": "limit"},
    "sk-mock-B": {"mode": "echo"}
  }
}

Modes:
  echo          reply "MOCK-REPLY[<account>] #<n>: <last user text>"
  limit         HTTP 429 with anthropic-ratelimit-unified-* headers (status rejected),
                x-should-retry: false
  transient429  HTTP 429 without unified headers, retry-after 1 (retryable throttle);
                after "count" (default 2) hits, falls back to echo
  tool_big      main-loop calls get a shell tool_use that prints ~120 KB; once the
                tool_result comes back, reply "MOCK-TOOL-DONE"
  tool_small    like tool_big but the command prints one short line
  slow          stream an echo reply slowly ("seconds", default 20) for mid-turn tests
  overloaded    HTTP 529 overloaded_error
  auth401       HTTP 401 authentication_error (an invalid/expired key)
  billing       HTTP 400 "credit balance is too low" (claude reports billing_error)

Optional per-account keys: "headers" (extra/override response headers),
"reset_in" (seconds until the unified reset, default 3600), "seconds", "count",
"text" (fixed reply text for echo/slow).

Every request is appended to the JSONL log (keys redacted to the last 4 chars).

CLI:  python -I mockapi.py --port 0 --log mock.jsonl --scenario scenario.json
      prints "LISTENING <port>" once ready.
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

UNKNOWN = "anon"


def _now() -> float:
    return time.time()


def _redact(key: str | None) -> str | None:
    if not key:
        return key
    return "..." + key[-4:] if len(key) > 4 else key


class MockState:
    def __init__(self, log_path: str, scenario_path: str | None):
        self.log_path = log_path
        self.scenario_path = scenario_path
        self.lock = threading.Lock()
        self.n = 0
        self.per_account_hits: dict[str, int] = {}
        os.makedirs(os.path.dirname(os.path.abspath(log_path)) or ".", exist_ok=True)

    def scenario_for(self, account: str) -> dict:
        data: dict = {}
        if self.scenario_path and os.path.exists(self.scenario_path):
            try:
                with open(self.scenario_path, encoding="utf-8") as f:
                    data = json.load(f)
            except (OSError, ValueError):
                data = {}
        acc = (data.get("accounts") or {}).get(account)
        return dict(acc or data.get("default") or {"mode": "echo"})

    def next_n(self, account: str) -> tuple[int, int]:
        with self.lock:
            self.n += 1
            self.per_account_hits[account] = self.per_account_hits.get(account, 0) + 1
            return self.n, self.per_account_hits[account]

    def log(self, entry: dict) -> None:
        line = json.dumps(entry, ensure_ascii=False)
        with self.lock:
            with open(self.log_path, "a", encoding="utf-8") as f:
                f.write(line + "\n")


def _last_user_text(body: dict) -> tuple[str, bool]:
    """Return (text of the last real user text block, last user msg has tool_result)."""
    msgs = body.get("messages") or []
    for m in reversed(msgs):
        if m.get("role") != "user":
            continue
        content = m.get("content")
        if isinstance(content, str):
            return content, False
        has_tool_result = False
        texts = []
        for block in content or []:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "tool_result":
                has_tool_result = True
            elif block.get("type") == "text":
                t = block.get("text") or ""
                if not t.lstrip().startswith("<system-reminder>"):
                    texts.append(t)
        return (texts[-1] if texts else ""), has_tool_result
    return "", False


def _snippet(text: str, n: int = 80) -> str:
    return " ".join(text.split())[:n]


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "MockAnthropic/0.1"
    state: MockState  # set on the class by MockServer

    def log_message(self, fmt, *args):  # silence default stderr logging
        pass

    # ---- helpers -------------------------------------------------------
    def _account(self) -> str:
        key = self.headers.get("x-api-key")
        if not key:
            auth = self.headers.get("authorization") or ""
            if auth.lower().startswith("bearer "):
                key = auth[7:].strip()
        return key or UNKNOWN

    def _read_body(self) -> dict:
        length = int(self.headers.get("content-length") or 0)
        raw = self.rfile.read(length) if length else b""
        try:
            return json.loads(raw.decode("utf-8")) if raw else {}
        except ValueError:
            return {"_unparsed": raw[:200].decode("utf-8", "replace")}

    def _send_json(self, status: int, obj: dict, extra_headers: dict | None = None) -> None:
        data = json.dumps(obj).encode("utf-8")
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(data)))
        self.send_header("request-id", f"req_mock_{int(_now()*1000)}")
        for k, v in (extra_headers or {}).items():
            self.send_header(k, str(v))
        self.end_headers()
        self.wfile.write(data)

    def _sse_start(self, extra_headers: dict | None = None) -> None:
        self.send_response(200)
        self.send_header("content-type", "text/event-stream")
        self.send_header("cache-control", "no-cache")
        self.send_header("connection", "close")
        self.send_header("request-id", f"req_mock_{int(_now()*1000)}")
        for k, v in (extra_headers or {}).items():
            self.send_header(k, str(v))
        self.end_headers()
        self.close_connection = True

    def _event(self, name: str, payload: dict) -> None:
        self.wfile.write(f"event: {name}\ndata: {json.dumps(payload)}\n\n".encode("utf-8"))
        self.wfile.flush()

    # ---- routes --------------------------------------------------------
    def do_GET(self):
        account = self._account()
        if self.path.startswith("/v1/models"):
            models = [
                {"id": m, "type": "model", "display_name": m, "created_at": "2026-01-01T00:00:00Z"}
                for m in ("claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5")
            ]
            self._send_json(200, {"data": models, "has_more": False,
                                  "first_id": models[0]["id"], "last_id": models[-1]["id"]})
            status = 200
        else:
            self._send_json(404, {"type": "error", "error": {"type": "not_found_error",
                                                              "message": "mock: unknown path"}})
            status = 404
        self.state.log({"ts": _now(), "method": "GET", "path": self.path,
                        "account": _redact(account), "status": status})

    def do_POST(self):
        account = self._account()
        body = self._read_body()
        path = self.path.split("?")[0]
        if path.endswith("/v1/messages/count_tokens"):
            approx = max(1, len(json.dumps(body)) // 4)
            self._send_json(200, {"input_tokens": approx})
            self.state.log({"ts": _now(), "method": "POST", "path": self.path,
                            "account": _redact(account), "status": 200, "kind": "count_tokens"})
            return
        if not path.endswith("/v1/messages"):
            self._send_json(404, {"type": "error", "error": {"type": "not_found_error",
                                                              "message": "mock: unknown path"}})
            self.state.log({"ts": _now(), "method": "POST", "path": self.path,
                            "account": _redact(account), "status": 404})
            return
        self._messages(account, body)

    def _messages(self, account: str, body: dict) -> None:
        n, hits = self.state.next_n(account)
        sc = self.state.scenario_for(account)
        mode = sc.get("mode", "echo")
        tools = [t.get("name") for t in (body.get("tools") or []) if isinstance(t, dict)]
        is_main = len(tools) > 0
        user_text, has_tool_result = _last_user_text(body)
        label = account[-1:] if account != UNKNOWN else "?"
        entry = {
            "ts": _now(), "n": n, "method": "POST", "path": self.path,
            "account": _redact(account), "mode": mode, "model": body.get("model"),
            "stream": bool(body.get("stream")), "main_loop": is_main,
            "n_messages": len(body.get("messages") or []), "n_tools": len(tools),
            "has_tool_result": has_tool_result, "user_text": _snippet(user_text, 200),
            "max_tokens": body.get("max_tokens"),
            "thinking": (body.get("thinking") or {}).get("type") if isinstance(body.get("thinking"), dict) else None,
            "headers": {k: self.headers.get(k) for k in (
                "anthropic-beta", "anthropic-version", "user-agent", "x-app",
                "x-claude-code-session-id", "x-stainless-retry-count") if self.headers.get(k)},
        }

        if mode == "transient429" and hits <= int(sc.get("count", 2)):
            entry["status"] = 429
            self.state.log(entry)
            hdrs = {"retry-after": "1", "x-should-retry": "true"}
            hdrs.update(sc.get("headers") or {})
            self._send_json(429, {"type": "error", "error": {
                "type": "rate_limit_error", "message": "mock transient throttle"}}, hdrs)
            return
        if mode == "transient429":
            mode = "echo"

        if mode == "limit":
            reset = int(_now() + int(sc.get("reset_in", 3600)))
            hdrs = {
                "anthropic-ratelimit-unified-status": "rejected",
                "anthropic-ratelimit-unified-reset": str(reset),
                "anthropic-ratelimit-unified-representative-claim": "five_hour",
                "anthropic-ratelimit-unified-5h-status": "rejected",
                "anthropic-ratelimit-unified-5h-utilization": "1.0",
                "anthropic-ratelimit-unified-5h-reset": str(reset),
                "anthropic-ratelimit-unified-7d-status": "allowed",
                "anthropic-ratelimit-unified-7d-utilization": "0.42",
                "anthropic-ratelimit-unified-7d-reset": str(reset + 86400 * 3),
                "anthropic-ratelimit-unified-fallback-percentage": "0.5",
                "x-should-retry": "false",
                "retry-after": str(int(sc.get("reset_in", 3600))),
            }
            hdrs.update(sc.get("headers") or {})
            entry["status"] = 429
            self.state.log(entry)
            self._send_json(429, {"type": "error", "error": {
                "type": "rate_limit_error",
                "message": "This request would exceed your account's rate limit. Please try again later."}}, hdrs)
            return

        if mode == "overloaded":
            entry["status"] = 529
            self.state.log(entry)
            self._send_json(529, {"type": "error", "error": {"type": "overloaded_error",
                                                              "message": "Overloaded"}},
                            {"x-should-retry": "false"})
            return

        if mode == "auth401":
            entry["status"] = 401
            self.state.log(entry)
            self._send_json(401, {"type": "error", "error": {"type": "authentication_error",
                                                              "message": "invalid x-api-key"}},
                            {"x-should-retry": "false"})
            return

        if mode == "billing":
            entry["status"] = 400
            self.state.log(entry)
            self._send_json(400, {"type": "error", "error": {
                "type": "invalid_request_error",
                "message": "Your credit balance is too low to access the Anthropic API. "
                           "Please go to Plans & Billing to upgrade or purchase credits."}},
                            {"x-should-retry": "false"})
            return

        model = body.get("model") or "claude-mock"
        stream = bool(body.get("stream"))
        if mode in ("tool_big", "tool_small") and is_main and not has_tool_result:
            shell = "Bash" if "Bash" in tools else ("PowerShell" if "PowerShell" in tools else None)
            if shell:
                if shell == "Bash":
                    cmd = ("head -c 120000 /dev/zero | tr '\\0' x" if mode == "tool_big"
                           else "echo MOCK-TOOL-OUTPUT")
                else:
                    cmd = "'x' * 120000" if mode == "tool_big" else "'MOCK-TOOL-OUTPUT'"
                tool_input = {"command": cmd, "description": "mock tool call"}
                entry.update(status=200, reply="tool_use", tool=shell)
                self.state.log(entry)
                self._reply(model, stream, tool_use=(shell, tool_input))
                return
            entry["note"] = f"no shell tool among {tools[:12]}"
        if mode in ("tool_big", "tool_small") and has_tool_result:
            text = "MOCK-TOOL-DONE"
        else:
            text = sc.get("text") or f"MOCK-REPLY[{label}] #{n}: {_snippet(user_text)}"
        delay = float(sc.get("seconds", 20)) if mode == "slow" else 0.0
        entry.update(status=200, reply="text", text=text[:200])
        self.state.log(entry)
        self._reply(model, stream, text=text, total_delay=delay)

    def _reply(self, model: str, stream: bool, text: str | None = None,
               tool_use: tuple[str, dict] | None = None, total_delay: float = 0.0) -> None:
        msg_id = f"msg_mock_{int(_now()*1000)}"
        usage = {"input_tokens": 12, "output_tokens": 1,
                 "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
        if not stream:
            content = ([{"type": "text", "text": text}] if text is not None else
                       [{"type": "tool_use", "id": f"toolu_mock_{int(_now()*1000)}",
                         "name": tool_use[0], "input": tool_use[1]}])
            self._send_json(200, {"id": msg_id, "type": "message", "role": "assistant",
                                  "model": model, "content": content,
                                  "stop_reason": "end_turn" if text is not None else "tool_use",
                                  "stop_sequence": None,
                                  "usage": {**usage, "output_tokens": 20}})
            return
        try:
            self._sse_start()
            self._event("message_start", {"type": "message_start", "message": {
                "id": msg_id, "type": "message", "role": "assistant", "model": model,
                "content": [], "stop_reason": None, "stop_sequence": None, "usage": usage}})
            if text is not None:
                self._event("content_block_start", {"type": "content_block_start", "index": 0,
                                                    "content_block": {"type": "text", "text": ""}})
                self._event("ping", {"type": "ping"})
                words = text.split(" ")
                chunks = [w + (" " if i < len(words) - 1 else "") for i, w in enumerate(words)]
                per = total_delay / max(1, len(chunks)) if total_delay else 0.0
                for c in chunks:
                    if per:
                        time.sleep(per)
                    self._event("content_block_delta", {"type": "content_block_delta", "index": 0,
                                                        "delta": {"type": "text_delta", "text": c}})
                self._event("content_block_stop", {"type": "content_block_stop", "index": 0})
                stop = "end_turn"
            else:
                name, tool_input = tool_use
                self._event("content_block_start", {"type": "content_block_start", "index": 0,
                                                    "content_block": {"type": "tool_use",
                                                                      "id": f"toolu_mock_{int(_now()*1000)}",
                                                                      "name": name, "input": {}}})
                self._event("content_block_delta", {"type": "content_block_delta", "index": 0,
                                                    "delta": {"type": "input_json_delta",
                                                              "partial_json": json.dumps(tool_input)}})
                self._event("content_block_stop", {"type": "content_block_stop", "index": 0})
                stop = "tool_use"
            self._event("message_delta", {"type": "message_delta",
                                          "delta": {"stop_reason": stop, "stop_sequence": None},
                                          "usage": {"output_tokens": 20}})
            self._event("message_stop", {"type": "message_stop"})
        except (BrokenPipeError, ConnectionResetError, ConnectionAbortedError):
            self.state.log({"ts": _now(), "event": "client_disconnected_mid_stream", "msg": msg_id})


class MockServer:
    """In-process mock server. Usage:
        srv = MockServer(log_path, scenario_path); port = srv.start(); ...; srv.stop()
    """

    def __init__(self, log_path: str, scenario_path: str | None = None, port: int = 0):
        self.state = MockState(log_path, scenario_path)
        handler = type("BoundHandler", (Handler,), {"state": self.state})
        self.httpd = ThreadingHTTPServer(("127.0.0.1", port), handler)
        self.httpd.daemon_threads = True
        self.thread: threading.Thread | None = None

    @property
    def port(self) -> int:
        return self.httpd.server_address[1]

    @property
    def base_url(self) -> str:
        return f"http://127.0.0.1:{self.port}"

    def start(self) -> int:
        self.thread = threading.Thread(target=self.httpd.serve_forever, daemon=True)
        self.thread.start()
        return self.port

    def stop(self) -> None:
        self.httpd.shutdown()
        self.httpd.server_close()


def write_scenario(path: str, accounts: dict[str, dict], default: dict | None = None) -> None:
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump({"default": default or {"mode": "echo"}, "accounts": accounts}, f, indent=1)
    os.replace(tmp, path)


def read_log(path: str) -> list[dict]:
    rows = []
    if not os.path.exists(path):
        return rows
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if line:
                try:
                    rows.append(json.loads(line))
                except ValueError:
                    pass
    return rows


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, default=0)
    ap.add_argument("--log", required=True)
    ap.add_argument("--scenario")
    a = ap.parse_args()
    srv = MockServer(a.log, a.scenario, a.port)
    port = srv.start()
    print(f"LISTENING {port}", flush=True)
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        srv.stop()
    return 0


if __name__ == "__main__":
    sys.exit(main())
