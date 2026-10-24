"""Exercise shell supervisor locking and heartbeat using disposable processes."""
from pathlib import Path
import os
import signal
import subprocess
import tempfile
import time

PROJECT = Path(__file__).resolve().parents[2]


def wait_for(predicate, message):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        if predicate():
            return
        time.sleep(.03)
    raise AssertionError(message)


def stop(proc, sig=signal.SIGTERM):
    if proc.poll() is None:
        os.killpg(proc.pid, sig)
        proc.wait(timeout=5)


with tempfile.TemporaryDirectory() as tmp:
    home = Path(tmp)
    base = home / 'xray'
    panel = base / 'panel'
    panel.mkdir(parents=True)
    runtime = home / 'darkls-'
    service = panel / 'service.sh'
    count = home / 'launches'
    service.write_text(f'#!/bin/sh\necho ensure >> "{count}"\n')
    script = (PROJECT / 'release/watch.sh').read_text().replace('/jffs/xray', str(base)).replace('/tmp/darkls-', str(runtime)).replace('sleep 15', 'sleep .1')
    # Some hosted executors expose a /proc view from another PID namespace.
    # Keep signal delivery real; provide the two identity files explicitly.
    (home / 'owner-cmdline').write_text(str(base / 'watch.sh'))
    (home / 'panel-cmdline').write_text(str(panel / 'darkls-panel'))
    uptime_file = home / 'uptime'
    uptime_file.write_text('10000.00 0.00\n')
    script = script.replace('/proc/uptime', str(uptime_file))
    script = script.replace('"/proc/$owner/cmdline"', '"' + str(home / 'owner-cmdline') + '"')
    script = script.replace('"/proc/$panel_pid/cmdline"', '"' + str(home / 'panel-cmdline') + '"')
    watch = base / 'watch.sh' 
    watch.write_text(script)
    pidfile = home / 'darkls-watch.pid'
    lock = home / 'darkls-watch.lock'
    logfile = home / 'watch.log'
    processes = []

    def start():
        with logfile.open('ab') as log:
            proc = subprocess.Popen(['sh', str(watch)], stdout=log, stderr=log, start_new_session=True)
        processes.append(proc)
        return proc

    try:
        first = start()
        wait_for(lambda: pidfile.exists() and count.exists(), 'initial supervisor did not start')
        duplicate = start()
        assert duplicate.wait(timeout=3) == 0
        assert pidfile.read_text().strip() == str(first.pid), 'duplicate stole owner lock'
        stop(first, signal.SIGKILL)
        assert lock.exists(), 'test did not leave a crash lock'
        replacement = start()
        wait_for(lambda: pidfile.exists() and pidfile.read_text().strip() == str(replacement.pid), 'stale lock blocked restart')
        stop(replacement)
        assert not lock.exists() and not pidfile.exists(), 'normal shutdown left its lock'
        print('PASS: duplicate suppression, SIGKILL stale lock recovery, owner-only cleanup')

        binary = panel / 'darkls-panel'
        stopped = home / 'stopped'
        binary.write_text(f'''#!/bin/sh
trap 'echo stopped > "{stopped}"; exit 0' TERM
while :; do sleep .1; done
''')
        fake = subprocess.Popen(['sh', str(binary)], start_new_session=True)
        processes.append(fake)
        (home / 'darkls-panel.pid').write_text(str(fake.pid))
        uptime = 10000
        heartbeat = home / 'darkls-monitor-heartbeat'
        heartbeat.write_text(f'{fake.pid} {uptime}\n')
        watcher = start()
        wait_for(lambda: pidfile.exists(), 'heartbeat supervisor did not start')
        time.sleep(.3)
        assert fake.poll() is None, 'fresh heartbeat killed the panel'
        heartbeat.write_text(f'{fake.pid} {uptime-241}\n')
        wait_for(lambda: stopped.exists(), 'stale monitor was not restarted')
        fake.wait(timeout=5)
        stop(watcher)
        assert 'restarting panel only' in logfile.read_text()
        print('PASS: fresh heartbeat preserved; expired heartbeat stops only the panel')
    finally:
        for proc in reversed(processes):
            stop(proc, signal.SIGKILL)
