"""Real Linux bridge/NAT/DNS with three clients; no Internet or VPN credentials.

Requires root, iproute2, iptables and network-namespace capability. CI runs this
without skips. This tests kernel forwarding primitives, not the XHTTP server or
ASUS firmware; the device soak procedure is documented separately.
"""
import concurrent.futures
import http.client
import http.server
import os
from pathlib import Path
import socket
import socketserver
import subprocess
import sys
import threading
import time

SCRIPT = str(Path(__file__).resolve())
PAYLOAD = b'darkls-network-test\n' * 8192


def backend():
    class HTTP(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            if self.path == '/fail':
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()
                return
            self.send_response(200)
            self.send_header('Content-Length', str(len(PAYLOAD)))
            self.end_headers()
            self.wfile.write(PAYLOAD)

        def log_message(self, *_):
            pass

    class DNS(socketserver.BaseRequestHandler):
        def handle(self):
            data, sock = self.request
            if b'slow' in data:
                time.sleep(.8)
            # A valid NXDOMAIN is evidence that the DNS path answered.
            sock.sendto(data[:2] + b'\x81\x83' + data[4:6] + b'\0' * 6 + data[12:], self.client_address)

    class UDP(socketserver.ThreadingUDPServer):
        daemon_threads = True

    dns = UDP(('0.0.0.0', 10535), DNS)
    threading.Thread(target=dns.serve_forever, daemon=True).start()
    http.server.ThreadingHTTPServer(('0.0.0.0', 12345), HTTP).serve_forever()


def fetch(path='/stream'):
    conn = http.client.HTTPConnection('198.51.100.7', 443, timeout=2)
    try:
        conn.request('GET', path)
        response = conn.getresponse()
        assert response.status == 200
        assert response.read() == PAYLOAD
    finally:
        conn.close()


def client(slow=False):
    def dns():
        name = b'\x04slow' if slow else b'\x04fast'
        packet = b'\x12\x34\x01\x00\x00\x01' + b'\0' * 6 + name + b'\x07example\x00\x00\x01\x00\x01'
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
            sock.settimeout(2)
            started = time.monotonic()
            sock.sendto(packet, ('8.8.8.8', 53))
            reply = sock.recv(512)
            assert reply[:2] == b'\x12\x34' and reply[3] & 15 == 3
            if not slow:
                assert time.monotonic() - started < .7, 'one slow client blocked other DNS replies'

    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        result = pool.submit(dns)
        for _ in range(8):
            fetch()
        result.result()


def main():
    assert os.geteuid() == 0, 'Run with sudo; this test never changes the host firewall'
    router = f'darkls-{os.getpid()}-router'
    clients = [f'darkls-{os.getpid()}-c{i}' for i in range(3)]
    created = []
    proc = None

    def run(*args):
        return subprocess.run(args, check=True, capture_output=True, text=True, timeout=15)

    def inside(ns, *args):
        return run('ip', 'netns', 'exec', ns, *args)

    try:
        for ns in [router, *clients]:
            run('ip', 'netns', 'add', ns)
            created.append(ns)
            run('ip', '-n', ns, 'link', 'set', 'lo', 'up')
        run('ip', '-n', router, 'link', 'add', 'br0', 'type', 'bridge')
        run('ip', '-n', router, 'addr', 'add', '192.168.50.1/24', 'dev', 'br0')
        run('ip', '-n', router, 'link', 'set', 'br0', 'up')
        for i, ns in enumerate(clients):
            run('ip', 'link', 'add', f'v{i}', 'netns', router, 'type', 'veth', 'peer', 'name', 'eth0', 'netns', ns)
            run('ip', '-n', router, 'link', 'set', f'v{i}', 'master', 'br0')
            run('ip', '-n', router, 'link', 'set', f'v{i}', 'up')
            run('ip', '-n', ns, 'addr', 'add', f'192.168.50.{11+i}/24', 'dev', 'eth0')
            run('ip', '-n', ns, 'link', 'set', 'eth0', 'up')
            run('ip', '-n', ns, 'route', 'add', 'default', 'via', '192.168.50.1')
        inside(router, 'iptables', '-t', 'nat', '-N', 'DV_NAT')
        for protocol, source, target in [('tcp', '443', '12345'), ('udp', '53', '10535')]:
            inside(router, 'iptables', '-t', 'nat', '-A', 'DV_NAT', '-p', protocol, '--dport', source, '-j', 'REDIRECT', '--to-ports', target)
        hook = ['PREROUTING', '-i', 'br0', '-s', '192.168.50.0/24', '-j', 'DV_NAT']
        inside(router, 'iptables', '-t', 'nat', '-A', *hook)
        proc = subprocess.Popen(['ip', 'netns', 'exec', router, sys.executable, SCRIPT, 'backend'])
        for _ in range(50):
            ready = inside(router, sys.executable, '-c', "import socket;s=socket.socket();s.settimeout(.2);print(s.connect_ex(('127.0.0.1',12345)));s.close()")
            if ready.stdout.strip() == '0':
                break
            time.sleep(.05)
        else:
            raise AssertionError('test backend did not start')
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
            jobs = [pool.submit(inside, ns, sys.executable, SCRIPT, 'client', str(i)) for i, ns in enumerate(clients)]
            for job in jobs:
                job.result()
        print('PASS: three concurrent clients, video-sized transfers, isolated slow DNS')
        # A backend failure is observable, and a later connection can recover.
        inside(clients[0], sys.executable, SCRIPT, 'expect-failure')
        inside(clients[1], sys.executable, SCRIPT, 'client', '1')
        print('PASS: refused upstream response does not poison other clients')
        inside(router, 'iptables', '-t', 'nat', '-D', *hook)
        failed = subprocess.run(['ip', 'netns', 'exec', clients[0], sys.executable, SCRIPT, 'fetch'], capture_output=True, timeout=5)
        assert failed.returncode != 0, 'missing hook was not detected'
        inside(router, 'iptables', '-t', 'nat', '-A', *hook)
        for ns in clients:
            inside(ns, sys.executable, SCRIPT, 'fetch')
        print('PASS: removed firewall hook breaks new connections; restored hook repairs all clients')
    finally:
        if proc:
            proc.terminate()
            proc.wait(timeout=5)
        for ns in reversed(created):
            subprocess.run(['ip', 'netns', 'del', ns], capture_output=True, timeout=10)


if __name__ == '__main__':
    mode = sys.argv[1] if len(sys.argv) > 1 else 'main'
    if mode == 'backend':
        backend()
    elif mode == 'client':
        client(sys.argv[2] == '0')
    elif mode == 'fetch':
        fetch()
    elif mode == 'expect-failure':
        try:
            fetch('/fail')
        except (OSError, http.client.HTTPException):
            pass
        else:
            raise AssertionError('backend refusal was hidden')
    else:
        main()
