"""Run a Playwright scenario and the Go preview in the same network namespace."""
import os
import pathlib
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import urllib.request

root = pathlib.Path(__file__).resolve().parents[1]
go = os.environ.get('GO_BIN', 'go')
node = os.environ.get('NODE_BIN', shutil.which('node') or 'node')
scenarios = sys.argv[1:] or ['browser-features.cjs', 'browser-advanced.cjs', 'browser-core.cjs', 'browser-manage.cjs']
for scenario in scenarios:
    with tempfile.TemporaryFile(mode='w+') as log:
        proc = subprocess.Popen(
            [go, 'test', '-run', '^TestBrowserHarness$', '-v'], cwd=root,
            env={**os.environ, 'DARKLS_BROWSER_PREVIEW': '1'},
            start_new_session=True, stdout=log, stderr=subprocess.STDOUT,
        )
        try:
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                try:
                    with urllib.request.urlopen('http://127.0.0.1:18088', timeout=1):
                        break
                except OSError:
                    if proc.poll() is not None:
                        log.seek(0)
                        raise RuntimeError(log.read())
                    time.sleep(.1)
            else:
                raise RuntimeError('Browser preview did not start')
            subprocess.run([node, str(root / scenario)], cwd=root, check=True, timeout=150)
        finally:
            try:
                os.killpg(proc.pid, signal.SIGTERM)
                proc.wait(timeout=5)
            except ProcessLookupError:
                pass
