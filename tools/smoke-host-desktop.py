#!/usr/bin/env python3
"""Offline Linux native-window smoke test, run under xvfb-run with Pillow/XCB."""
import argparse
import os
from pathlib import Path
import subprocess
import tempfile
import time
from PIL import ImageGrab

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('binary', type=Path)
parser.add_argument('screenshot', type=Path)
args = parser.parse_args()
with tempfile.TemporaryDirectory(prefix='pbsx-desktop-smoke-') as settings:
    environment = {**os.environ, 'XDG_CONFIG_HOME': settings, 'FYNE_PLATFORM': 'x11', 'XDG_SESSION_TYPE': 'x11'}
    environment.pop('WAYLAND_DISPLAY', None)
    # Probe the X server before starting GLFW. -noreset in the workflow keeps
    # short-lived screenshot connections from resetting a display during startup.
    ImageGrab.grab(xdisplay=os.environ['DISPLAY'])
    app = subprocess.Popen([str(args.binary.resolve()), '--demo'], env=environment)
    try:
        deadline = time.monotonic()+30
        while True:
            if app.poll() is not None:
                raise RuntimeError('desktop exited before rendering')
            picture = ImageGrab.grab(xdisplay=os.environ['DISPLAY'])
            colors = picture.getcolors(maxcolors=256)
            if colors is None or len(colors) >= 20:
                break
            if time.monotonic() >= deadline:
                args.screenshot.parent.mkdir(parents=True, exist_ok=True)
                picture.save(args.screenshot)
                raise RuntimeError('desktop did not render a window within 30 seconds')
            time.sleep(0.5)
        args.screenshot.parent.mkdir(parents=True, exist_ok=True)
        picture.save(args.screenshot)
        print('PASS: offline native desktop rendered; screenshot:', args.screenshot)
    finally:
        app.terminate()
        app.wait(timeout=10)
