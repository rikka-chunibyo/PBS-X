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
    app = subprocess.Popen([str(args.binary.resolve()), '--demo'], env={**os.environ, 'XDG_CONFIG_HOME': settings})
    try:
        time.sleep(5)
        if app.poll() is not None:
            raise RuntimeError('desktop exited before rendering')
        picture = ImageGrab.grab(xdisplay=os.environ['DISPLAY'])
        args.screenshot.parent.mkdir(parents=True, exist_ok=True)
        picture.save(args.screenshot)
        if len(picture.getcolors(maxcolors=1000000) or []) < 20:
            raise RuntimeError('desktop did not render a window')
        print('PASS: offline native desktop rendered; screenshot:', args.screenshot)
    finally:
        app.terminate()
        app.wait(timeout=10)
