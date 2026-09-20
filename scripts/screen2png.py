#!/usr/bin/env python3
"""Render a terminal transcript (text file) as a PNG screenshot.

Usage: screen2png.py <input.txt> <output.png> [title]
Uses the headless Chromium shipped with Playwright; no Python packages needed.
"""
import html, os, pathlib, shutil, subprocess, sys, tempfile

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
.g{{color:#4ade80}} .y{{color:#facc15}} .r{{color:#f87171}} .gr{{color:#8b93a5}} .h{{color:#7dd3fc;font-weight:bold}}
.p{{color:#c084fc}} .w{{color:#fb923c}}
</style></head><body><div class="win"><div class="bar">
<span class="dot" style="background:#ff5f57"></span><span class="dot" style="background:#febc2e"></span>
<span class="dot" style="background:#28c840"></span>&nbsp;{title}</div><pre>{body}</pre></div></body></html>"""

def colorize(text: str) -> str:
    out = []
    for line in text.split("\n"):
        e = html.escape(line)
        s = line.strip()
        if s.startswith("$ "):
            e = '<span class="p">' + e + "</span>"
        elif line and not line.startswith(" ") and (s.isupper() or s.startswith("SYSTEM ") or s.startswith("skm ")):
            e = '<span class="h">' + e + "</span>"
        elif s.startswith("- ") or s.startswith("! "):
            e = '<span class="w">' + e + "</span>"
        for word, cls in (("GREEN", "g"), ("YELLOW", "y"), ("RED", "r"), ("GRAY", "gr")):
            e = e.replace(" " + word, ' <span class="%s">%s</span>' % (cls, word))
        out.append(e)
    return "\n".join(out)

def main() -> int:
    if len(sys.argv) < 3:
        print(__doc__); return 2
    src, dst = sys.argv[1], sys.argv[2]
    title = sys.argv[3] if len(sys.argv) > 3 else pathlib.Path(src).stem
    if not CHROME:
        print("screen2png: no chromium found", file=sys.stderr); return 1
    text = pathlib.Path(src).read_text(encoding="utf-8").rstrip("\n")
    lines = text.split("\n")
    width = min(1400, max(760, 22 + int(max(len(l) for l in lines) * 8.6) + 60))
    height = len(lines) * 20 + 36 + 34 + 32 + 90  # headless window-size includes UI chrome; keep a safety margin
    page = TEMPLATE.format(title=html.escape(title), body=colorize(text))
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
