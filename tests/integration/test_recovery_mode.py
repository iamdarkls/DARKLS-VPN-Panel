"""A failed startup rollback must leave an authenticated, read-only panel."""
import http.client
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import tempfile
import time

binary = os.environ['DARKLS_CURRENT_BINARY']
pidfile = Path('/tmp/darkls-panel.pid')
previous_pid = pidfile.read_bytes() if pidfile.exists() else None
with tempfile.TemporaryDirectory() as tmp, tempfile.TemporaryFile() as log:
    root = Path(tmp)
    for name, data in {'domains.txt': 'example.com\n', 'telegram-networks.txt': '', 'dnsmasq-vpn.conf': 'port=10535\n', 'autostart.disabled': ''}.items():
        (root/name).write_text(data)
    initialized = subprocess.check_output([binary, '--root', tmp, '--init'], text=True)
    password = re.search(r'Пароль: (\S+)', initialized)[1]
    journal = root/'panel/xray-update-journal.json'
    journal.write_text(json.dumps({'backup': str(root/'missing-usb/xray'), 'hash': '0'*64}))
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    host = f'127.0.0.1:{port}'
    proc = subprocess.Popen([binary, '--root', tmp, '--listen', host], stdout=log, stderr=log)
    def request(method, path, body=None, auth=False):
        headers = {'Origin': 'http://'+host, 'Content-Type': 'application/json'}
        if auth:
            headers.update({'Cookie': cookie, 'X-CSRF-Token': csrf})
        conn = http.client.HTTPConnection('127.0.0.1', port, timeout=5)
        conn.request(method, '/api/'+path, None if body is None else json.dumps(body), headers)
        response = conn.getresponse()
        status, data, session_cookie = response.status, json.loads(response.read()), response.getheader('Set-Cookie')
        conn.close()
        return status, data, session_cookie
    try:
        for _ in range(100):
            try:
                request('GET', 'update-status')
                break
            except OSError:
                if proc.poll() is not None:
                    raise AssertionError('Panel exited instead of serving recovery UI')
                time.sleep(.05)
        else:
            raise AssertionError('Recovery panel did not start')
        code, data, session_cookie = request('POST', 'login', {'user': 'admin', 'password': password})
        assert code == 200
        cookie, csrf = session_cookie.split(';')[0], data['csrf']
        assert request('GET', 'status', auth=True)[1]['recovery_pending']
        assert request('GET', 'state', auth=True)[0] == 200
        assert request('POST', 'power', {'enabled': True}, auth=True)[0] == 409
        assert request('POST', 'recovery', {'password': 'wrong'}, auth=True)[0] == 403
        assert request('POST', 'recovery', {'password': password}, auth=True)[0] == 409
        assert journal.exists()
        # Emulate externally completed recovery; no USB or live VPN is touched.
        journal.unlink()
        assert request('POST', 'recovery', {'password': password}, auth=True)[0] == 200
        assert not request('GET', 'status', auth=True)[1]['recovery_pending']
        print('PASS: startup rollback failure keeps login/reads available, blocks writes, retains journal and supports authenticated retry')
    finally:
        proc.terminate()
        proc.wait(timeout=5)
        if previous_pid is None:
            pidfile.unlink(missing_ok=True)
        else:
            pidfile.write_bytes(previous_pid)
