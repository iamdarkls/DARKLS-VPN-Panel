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

root = pathlib.Path(__file__).resolve().parents[2]
go = os.environ.get('GO_BIN', 'go')
node = os.environ.get('NODE_BIN', shutil.which('node') or 'node')
scenarios = sys.argv[1:] or ['browser-features.cjs', 'browser-advanced.cjs', 'browser-core.cjs', 'browser-manage.cjs', 'browser-organization.cjs', 'browser-assets.cjs', 'browser-recovery.cjs', 'browser-vue.cjs', 'browser-design.cjs', 'browser-workspace.cjs', 'browser-autofill.cjs', 'browser-catalog.cjs', 'browser-subscription.cjs']
# Compile once, then run a fresh process per scenario to keep fixtures isolated.
# Tests remain the same; repeated Go package loading and compilation are avoided.
with tempfile.TemporaryDirectory(prefix='darkls-browser-') as build_dir:
    harness = pathlib.Path(build_dir) / 'panel.test'
    subprocess.run(
        [go, 'test', '-c', '-o', str(harness), './internal/panel'],
        cwd=root, check=True,
    )
    for scenario in scenarios:
        with tempfile.TemporaryFile(mode='w+') as log:
            proc = subprocess.Popen(
                [str(harness), '-test.run=^TestBrowserHarness$', '-test.v', '-test.timeout=240s'], cwd=root,
                env={**os.environ, 'DARKLS_BROWSER_PREVIEW': '1',
                     'DARKLS_BROWSER_SUBSCRIPTION': '1' if scenario == 'browser-subscription.cjs' else '0'},
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
                subprocess.run([node, str(root / 'tests/browser' / scenario)], cwd=root, check=True, timeout=150)
            finally:
                try:
                    os.killpg(proc.pid, signal.SIGTERM)
                    try:
                        proc.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        os.killpg(proc.pid, signal.SIGKILL)
                        proc.wait()
                except ProcessLookupError:
                    pass
