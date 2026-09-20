#!/usr/bin/env python3
"""Render a terminal transcript (text with ANSI colours) as a PNG screenshot.

Usage: screen2png.py <input.txt> <output.png> [title]
Uses the headless Chromium shipped with Playwright; no Python packages needed.
"""
import html, os, pathlib, re, shutil, subprocess, sys, tempfile

CHROME = next((p for p in (
    os.environ.get("CHROME_BIN", ""),
    "/opt/pw-browsers/chromium/chrome-linux/chrome",
    *sorted(str(p) for p in pathlib.Path("/opt/pw-browsers").glob("chromium-*/chrome-linux/chrome")),
    shutil.which("chromium") or "", shutil.which("google-chrome") or "",
) if p and os.path.exists(p)), None)

TEMPLATE = """<!doctype html><html><head><meta charset="utf-8"><style>
body{{margin:0;background:#0f1117;font-family:"JetBrains Mono","DejaVu Sans Mono",Menlo,monospace;}}
.win{{margin:16px;border-radius:10px;overflow:hidden;background:#161a23;box-shadow:0 8px 30px rgba(0,0,0,.6);}}
.bar{{height:34px;background:#232838;display:flex;align-items:center;padding:0 14px;color:#9aa4b5;font-size:13px;}}
.dot{{width:12px;height:12px;border-radius:50%;margin-right:8px;display:inline-block;}}
pre{{margin:0;padding:18px 22px;color:#d6dbe5;font-size:14px;line-height:20px;white-space:pre;}}
.b{{font-weight:bold;color:#fff}} .d{{color:#7c8494}} .prompt{{color:#c084fc}}
.c30{{color:#6b7280}} .c31{{color:#f87171}} .c32{{color:#4ade80}} .c33{{color:#facc15}} .c34{{color:#60a5fa}}
.c35{{color:#c084fc}} .c36{{color:#7dd3fc}} .c37{{color:#e5e7eb}} .c90{{color:#7c8494}} .c91{{color:#fca5a5}}
.c92{{color:#86efac}} .c93{{color:#fde047}} .c94{{color:#93c5fd}} .c95{{color:#d8b4fe}} .c96{{color:#a5f3fc}} .c97{{color:#fff}}
</style></head><body><div class="win"><div class="bar">
<span class="dot" style="background:#ff5f57"></span><span class="dot" style="background:#febc2e"></span>
<span class="dot" style="background:#28c840"></span>&nbsp;{title}</div><pre>{body}</pre></div></body></html>"""

SGR = re.compile(r"\x1b\[([0-9;]*)m")

def ansi_to_html(text: str) -> str:
    """Convert SGR colour/bold/dim sequences into spans; escape everything else."""
    out, classes, pos = [], [], 0
    def open_span():
        return '<span class="%s">' % " ".join(classes) if classes else ""
    def close_span():
        return "</span>" if classes else ""
    for m in SGR.finditer(text):
        out.append(html.escape(text[pos:m.start()]))
        pos = m.end()
        out.append(close_span())
        for code in (m.group(1) or "0").split(";"):
            if code in ("", "0"):
                classes = []
            elif code == "1":
                classes = [c for c in classes if c != "b"] + ["b"]
            elif code == "2":
                classes = [c for c in classes if c != "d"] + ["d"]
            elif code == "39":
                classes = [c for c in classes if not c.startswith("c")]
            elif code.isdigit() and (30 <= int(code) <= 37 or 90 <= int(code) <= 97):
                classes = [c for c in classes if not c.startswith("c")] + ["c" + code]
        out.append(open_span())
    out.append(html.escape(text[pos:]))
    out.append(close_span())
    return "".join(out)

def render_lines(text: str) -> str:
    lines = []
    for line in text.split("\n"):
        if line.startswith("$ "):
            lines.append('<span class="prompt">' + html.escape(line) + "</span>")
        else:
            lines.append(ansi_to_html(line))
    return "\n".join(lines)

def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__); return 2
    src, dst = sys.argv[1], sys.argv[2]
    title = sys.argv[3] if len(sys.argv) > 3 else pathlib.Path(src).stem
    if not CHROME:
        print("screen2png: no chromium found", file=sys.stderr); return 1
    text = pathlib.Path(src).read_text(encoding="utf-8").rstrip("\n")
    plain = [SGR.sub("", l) for l in text.split("\n")]
    width = min(1400, max(760, 22 + int(max(len(l) for l in plain) * 8.6) + 60))
    height = len(plain) * 20 + 36 + 34 + 32 + 90  # window-size includes UI chrome; keep a margin
    page = TEMPLATE.format(title=html.escape(title), body=render_lines(text))
    with tempfile.TemporaryDirectory() as tmp:
        f = pathlib.Path(tmp, "screen.html"); f.write_text(page, encoding="utf-8")
        cmd = [CHROME, "--headless=new", "--no-sandbox", "--disable-gpu", "--hide-scrollbars",
               "--force-device-scale-factor=2", f"--window-size={width},{height}",
               f"--screenshot={os.path.abspath(dst)}", f.as_uri()]
        subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, cwd=tmp)
    print(dst)
    return 0

if __name__ == "__main__":
    sys.exit(main())
