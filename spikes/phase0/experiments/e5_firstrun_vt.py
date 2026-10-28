"""Tiny VT screen model for e5_firstrun (stdlib only).

Feeds ConPTY output bytes and keeps a cols x rows character grid (main + alternate
screen) so the experiment can snapshot what is actually on screen at a moment,
instead of the cursor-jumbled stripped text. Handles the subset ConPTY emits:
CUP/CUU/CUD/CUF/CUB/CHA/VPA/CNL/CPL, ED, EL, ECH, ICH, DCH, IL, DL, SU, SD,
DECSTBM, DECSC/DECRC, RI, IND/NEL, SGR (ignored), DECSET 1049/47/1047, OSC/DCS (skipped).
"""
from __future__ import annotations

import codecs
import unicodedata


def _wcwidth(ch: str) -> int:
    if unicodedata.combining(ch):
        return 0
    o = ord(ch)
    if o < 32:
        return 0
    if unicodedata.east_asian_width(ch) in ("W", "F"):
        return 2
    return 1


class Screen:
    def __init__(self, cols: int = 120, rows: int = 40):
        self.cols, self.rows = cols, rows
        self.main = self._blank()
        self.alt = self._blank()
        self.buf = self.main
        self.using_alt = False
        self.x = self.y = 0
        self.saved = (0, 0)
        self.top, self.bot = 0, rows - 1
        self._dec = codecs.getincrementaldecoder("utf-8")("replace")
        self._pending = ""   # incomplete escape sequence carried between feeds
        self.wrap_pending = False

    def _blank(self):
        return [[" "] * self.cols for _ in range(self.rows)]

    def _blank_row(self):
        return [" "] * self.cols

    # ---- public --------------------------------------------------------
    def feed(self, data: bytes) -> None:
        s = self._pending + self._dec.decode(data)
        self._pending = ""
        i, n = 0, len(s)
        while i < n:
            ch = s[i]
            if ch == "\x1b":
                j = self._escape(s, i)
                if j is None:          # incomplete: keep for next feed
                    self._pending = s[i:]
                    return
                i = j
                continue
            if ch in "\r\n\b\x0b\x0c":
                self.wrap_pending = False
            if ch == "\r":
                self.x = 0
            elif ch == "\n" or ch == "\x0b" or ch == "\x0c":
                self._lf()
            elif ch == "\b":
                self.x = max(0, self.x - 1)
            elif ch == "\t":
                self.x = min(self.cols - 1, (self.x // 8 + 1) * 8)
            elif ch == "\x07" or ord(ch) < 32:
                pass
            else:
                self._put(ch)
            i += 1

    def lines(self) -> list[str]:
        return ["".join(c for c in row if c is not None).rstrip() for row in self.buf]

    def text(self) -> str:
        ls = self.lines()
        while ls and not ls[-1]:
            ls.pop()
        return "\n".join(ls)

    # ---- internals -----------------------------------------------------
    def _put(self, ch: str) -> None:
        w = _wcwidth(ch)
        if w == 0:
            return
        if self.wrap_pending or self.x + w > self.cols:
            self.wrap_pending = False
            self.x = 0
            self._lf()
        self.buf[self.y][self.x] = ch
        if w == 2 and self.x + 1 < self.cols:
            self.buf[self.y][self.x + 1] = None   # placeholder for the wide char's 2nd cell
        self.x += w
        if self.x >= self.cols:   # deferred wrap: the next printable char wraps
            self.x = self.cols - 1
            self.wrap_pending = True

    def _lf(self) -> None:
        if self.y == self.bot:
            self._scroll_up(1)
        elif self.y < self.rows - 1:
            self.y += 1

    def _scroll_up(self, k: int) -> None:
        for _ in range(k):
            del self.buf[self.top]
            self.buf.insert(self.bot, self._blank_row())

    def _scroll_down(self, k: int) -> None:
        for _ in range(k):
            del self.buf[self.bot]
            self.buf.insert(self.top, self._blank_row())

    def _clip(self) -> None:
        self.x = max(0, min(self.cols - 1, self.x))
        self.y = max(0, min(self.rows - 1, self.y))

    def _escape(self, s: str, i: int):
        n = len(s)
        if i + 1 >= n:
            return None
        c = s[i + 1]
        if c == "[":
            j = i + 2
            while j < n and not ("@" <= s[j] <= "~"):
                j += 1
            if j >= n:
                return None
            self._csi(s[i + 2:j], s[j])
            return j + 1
        if c == "]":  # OSC ... BEL or ST
            j = i + 2
            while j < n:
                if s[j] == "\x07":
                    return j + 1
                if s[j] == "\x1b" and j + 1 < n and s[j + 1] == "\\":
                    return j + 2
                j += 1
            return None
        if c in "PX^_":  # DCS/SOS/PM/APC ... ST
            j = s.find("\x1b\\", i + 2)
            return None if j < 0 else j + 2
        if c in "()*+":
            return i + 3 if i + 2 < n else None
        if c == "7":
            self.saved = (self.x, self.y)
        elif c == "8":
            self.x, self.y = self.saved
        elif c == "M":  # reverse index
            if self.y == self.top:
                self._scroll_down(1)
            else:
                self.y = max(0, self.y - 1)
        elif c == "D":
            self._lf()
        elif c == "E":
            self.x = 0
            self._lf()
        elif c == "c":
            self.main, self.alt = self._blank(), self._blank()
            self.buf = self.alt if self.using_alt else self.main
            self.x = self.y = 0
        return i + 2

    def _csi(self, params: str, final: str) -> None:
        private = params[:1] in ("?", ">", "<", "=")
        body = params[1:] if private else params
        body = body.rstrip(" !\"#$%&'()*+,-./")
        try:
            nums = [int(p) if p else 0 for p in body.split(";")] if body else []
        except ValueError:
            nums = []

        def a(idx=0, default=1):
            v = nums[idx] if idx < len(nums) else 0
            return v if v else default

        if private:
            if params[0] == "?" and final in "hl":
                for p in nums:
                    if p in (1049, 47, 1047):
                        on = final == "h"
                        if on and not self.using_alt:
                            if p == 1049:
                                self.saved = (self.x, self.y)
                            self.alt = self._blank()
                            self.buf = self.alt
                            self.using_alt = True
                        elif not on and self.using_alt:
                            self.buf = self.main
                            self.using_alt = False
                            if p == 1049:
                                self.x, self.y = self.saved
            return
        if final in "Hf":
            self.y, self.x = a(0) - 1, a(1) - 1
        elif final == "A":
            self.y -= a()
        elif final == "B":
            self.y += a()
        elif final == "C":
            self.x += a()
        elif final == "D":
            self.x -= a()
        elif final == "E":
            self.y += a(); self.x = 0
        elif final == "F":
            self.y -= a(); self.x = 0
        elif final == "G" or final == "`":
            self.x = a() - 1
        elif final == "d":
            self.y = a() - 1
        elif final == "J":
            m = nums[0] if nums else 0
            self._clip()
            if m == 0:
                self.buf[self.y][self.x:] = [" "] * (self.cols - self.x)
                for r in range(self.y + 1, self.rows):
                    self.buf[r] = self._blank_row()
            elif m == 1:
                self.buf[self.y][: self.x + 1] = [" "] * (self.x + 1)
                for r in range(0, self.y):
                    self.buf[r] = self._blank_row()
            else:
                for r in range(self.rows):
                    self.buf[r] = self._blank_row()
        elif final == "K":
            m = nums[0] if nums else 0
            self._clip()
            row = self.buf[self.y]
            if m == 0:
                row[self.x:] = [" "] * (self.cols - self.x)
            elif m == 1:
                row[: self.x + 1] = [" "] * (self.x + 1)
            else:
                self.buf[self.y] = self._blank_row()
        elif final == "X":
            self._clip()
            k = a()
            row = self.buf[self.y]
            for c in range(self.x, min(self.cols, self.x + k)):
                row[c] = " "
        elif final == "@":
            self._clip()
            k = a()
            row = self.buf[self.y]
            row[self.x:self.x] = [" "] * k
            del row[self.cols:]
        elif final == "P":
            self._clip()
            k = a()
            row = self.buf[self.y]
            del row[self.x:self.x + k]
            row.extend([" "] * (self.cols - len(row)))
        elif final == "L":
            self._clip()
            if self.top <= self.y <= self.bot:
                for _ in range(a()):
                    del self.buf[self.bot]
                    self.buf.insert(self.y, self._blank_row())
        elif final == "M":
            self._clip()
            if self.top <= self.y <= self.bot:
                for _ in range(a()):
                    del self.buf[self.y]
                    self.buf.insert(self.bot, self._blank_row())
        elif final == "S":
            self._scroll_up(a())
        elif final == "T":
            self._scroll_down(a())
        elif final == "r":
            t = a(0) - 1
            b = (nums[1] if len(nums) > 1 and nums[1] else self.rows) - 1
            if 0 <= t < b < self.rows:
                self.top, self.bot = t, b
            else:
                self.top, self.bot = 0, self.rows - 1
            self.x = self.y = 0
        elif final == "s":
            self.saved = (self.x, self.y)
        elif final == "u":
            self.x, self.y = self.saved
        # SGR 'm', DSR 'n', DA 'c', 'q', 't' etc.: ignored
        if final != "m":
            self.wrap_pending = False
        self._clip()
