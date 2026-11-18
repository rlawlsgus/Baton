"""Checks of the E2E script itself that need no build, no claude.exe and no mock server; they read the
page's code from the repo (internal/gui/web/app.js) and the E2E's own source:

    python -I test/e2e/test_selfcheck.py
"""
from __future__ import annotations

import ast
import os
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)  # python -I leaves the script's own folder off the path
import e2e_baton as e2e  # noqa: E402
from cc import isolated_env  # noqa: E402  (e2e_baton put the harness's folder on the path)

APP_JS = os.path.join(e2e.REPO, "internal", "gui", "web", "app.js")


def read(path: str) -> str:
    with open(path, encoding="utf-8") as f:
        return f.read()


class LoginNotice(unittest.TestCase):
    """login_notice_problems, which scenario_desklogin runs on the page the window serves."""

    def setUp(self) -> None:
        self.js = read(APP_JS)

    def edited(self, old: str, new: str) -> str:
        self.assertIn(old, self.js, "app.js changed: update this test's edit")
        return self.js.replace(old, new, 1)

    def test_the_page_keeps_its_promise(self) -> None:
        self.assertEqual(e2e.login_notice_problems(self.js), [])

    def test_edits_that_break_it_are_found(self) -> None:
        edits = {
            # Pre-ticked as openNotice does from risk_acknowledged.
            "pre-ticked": ("h('input', { type: 'checkbox', onchange:",
                           "h('input', { type: 'checkbox', checked: !!S.state.config.desktop_logins_ack, onchange:"),
            "ticked by code": ("  openDialog(h('h2', { text: t('dl_notice_title') }),",
                               "  ack.checked = true;\n  openDialog(h('h2', { text: t('dl_notice_title') }),"),
            "Turn on enabled": ("text: t('dl_on'), disabled: true, onclick:", "text: t('dl_on'), onclick:"),
            "ticking does nothing": ("onchange: (e) => { go.disabled = !e.target.checked; }", "onchange: () => {}"),
            "renamed": ("\nfunction openLoginsNotice(", "\nfunction openDesktopNotice("),
        }
        for name, (old, new) in edits.items():
            with self.subTest(name):
                self.assertNotEqual(e2e.login_notice_problems(self.edited(old, new)), [])


class ClaudeEnv(unittest.TestCase):
    """A Claude Code an E2E scenario starts itself gets the run's scratch folders, never the real ones."""

    def test_scratch_folders(self) -> None:
        w = object.__new__(e2e.World)  # only the fields claude_env reads; no mock server
        with tempfile.TemporaryDirectory() as d:
            w.local_appdata, w.appdata = os.path.join(d, "localappdata"), os.path.join(d, "appdata")
            w.extra_env = {"TEMP": os.path.join(d, "temp"), "TMP": os.path.join(d, "temp")}
            env = isolated_env(os.path.join(d, "home"), None, w.claude_env(CLAUDE_CODE_ENTRYPOINT="claude-desktop"))
            self.assertEqual((env["LOCALAPPDATA"], env["APPDATA"]), (w.local_appdata, w.appdata))
            self.assertEqual((env["TEMP"], env["TMP"]), (w.extra_env["TEMP"], w.extra_env["TMP"]))
            self.assertEqual(env["CLAUDE_CODE_ENTRYPOINT"], "claude-desktop")

    def test_every_claude_the_e2e_starts_gets_them(self) -> None:
        """Every w.run.pty / w.run.run_print (claude.exe, outside baton) passes extra_env=w.claude_env(...):
        without it isolated_env hands it the machine's real %LOCALAPPDATA% and %APPDATA%."""
        tree = ast.parse(read(e2e.__file__))
        calls = [n for n in ast.walk(tree) if isinstance(n, ast.Call) and isinstance(n.func, ast.Attribute)
                 and n.func.attr in ("pty", "run_print")
                 and isinstance(n.func.value, ast.Attribute) and n.func.value.attr == "run"]
        self.assertTrue(calls, "no w.run.pty call found: update this test")

        def scratch(c: ast.Call) -> bool:
            return any(k.arg == "extra_env" and isinstance(k.value, ast.Call) and isinstance(k.value.func, ast.Attribute)
                       and k.value.func.attr == "claude_env" for k in c.keywords)

        self.assertEqual([c.lineno for c in calls if not scratch(c)], [],
                         "these lines start a Claude Code without extra_env=w.claude_env(...)")


if __name__ == "__main__":
    unittest.main()
