# Render the graphics in docs/assets/src/*.html to PNGs in docs/assets/ at 2x
# for sharp text, with an exact viewport. Fonts are vendored, so no network
# is needed.
#
#   python3 docs/assets/render.py [name...]        (default: all)
#
# Needs: pip install playwright (and a Chromium; CHROME=/path overrides).
import os, sys
from playwright.sync_api import sync_playwright

here = os.path.dirname(os.path.abspath(__file__))
targets = {  # name: (width, height) in CSS pixels
    "banner": (1280, 400),
    "social-preview": (1280, 640),
    "how-it-works": (1280, 720),
}
names = sys.argv[1:] or list(targets)
with sync_playwright() as p:
    b = p.chromium.launch(executable_path=os.environ.get("CHROME") or None, args=["--no-sandbox", "--allow-file-access-from-files"])
    for name in names:
        w, h = targets[name]
        pg = b.new_page(viewport={"width": w, "height": h}, device_scale_factor=2)
        pg.goto("file://" + os.path.join(here, "src", name + ".html"))
        pg.wait_for_timeout(400)
        pg.screenshot(path=os.path.join(here, name + ".png"))
        pg.close()
        print("rendered", name + ".png")
    b.close()
