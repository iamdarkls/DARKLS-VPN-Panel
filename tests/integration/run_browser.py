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
scenarios = sys.argv[1:] or ['browser-features.cjs', 'browser-advanced.cjs', 'browser-core.cjs', 'browser-manage.cjs', 'browser-organization.cjs', 'browser-recovery.cjs', 'browser-vue.cjs', 'browser-design.cjs', 'browser-workspace.cjs', 'browser-autofill.cjs', 'browser-catalog.cjs', 'browser-subscription.cjs']
def wait_for_preview(proc, log):
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if proc.poll() is not None:
            log.seek(0)
            raise RuntimeError(log.read())
        try:
            with urllib.request.urlopen('http://127.0.0.1:18088', timeout=1):
                return
        except OSError:
            time.sleep(.1)
    log.seek(0)
    raise RuntimeError('Browser preview did not start:\n' + log.read())


def stop_preview(proc):
    if proc.poll() is not None:
        return
    try:
        os.killpg(proc.pid, signal.SIGTERM)
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait()
    except ProcessLookupError:
        proc.wait()


def run_scenario(harness, scenario):
    with tempfile.TemporaryFile(mode='w+') as log:
        env = {
            **os.environ,
            'DARKLS_BROWSER_PREVIEW': '1',
            'DARKLS_BROWSER_SUBSCRIPTION': '1' if scenario == 'browser-subscription.cjs' else '0',
        }
        proc = subprocess.Popen(
            [str(harness), '-test.run=^TestBrowserHarness$', '-test.v', '-test.timeout=240s'],
            cwd=root, env=env, start_new_session=True, stdout=log, stderr=subprocess.STDOUT,
        )
        try:
            wait_for_preview(proc, log)
            subprocess.run([node, str(root / 'tests/browser' / scenario)], cwd=root, check=True, timeout=150)
        finally:
            stop_preview(proc)


def main():
    # Compile once, then start a clean fixture for each selected scenario.
    with tempfile.TemporaryDirectory(prefix='darkls-browser-') as build_dir:
        harness = pathlib.Path(build_dir) / 'panel.test'
        subprocess.run(
            [go, 'test', '-c', '-o', str(harness), './internal/panel'],
            cwd=root, check=True,
        )
        for scenario in scenarios:
            run_scenario(harness, scenario)


if __name__ == '__main__':
    main()
