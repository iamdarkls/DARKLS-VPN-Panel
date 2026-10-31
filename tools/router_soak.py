#!/usr/bin/env python3
"""Read-only LAN router observations while real devices generate traffic.

No VPN restarts, configuration changes or synthetic downloads. Passwords/cookies
stay in memory; only an allowlist of booleans and numeric metrics is recorded.
"""
import argparse
import getpass
import http.cookiejar
import json
from pathlib import Path
import time
import urllib.error
import urllib.parse
import urllib.request


def summarize(samples, interval):
    known = [s for s in samples if 'active' in s and not s.get('disabled')]
    panels = [s['panel_rss_kib'] for s in samples if s.get('panel_rss_kib') is not None]
    xrays = [s['xray_rss_kib'] for s in samples if s.get('xray_rss_kib') is not None]
    run = longest = 0
    for sample in samples:
        if sample.get('active') is False and not sample.get('disabled'):
            run += 1
            longest = max(longest, run)
        else:
            run = 0
    return {'samples': len(samples), 'known_enabled_samples': len(known),
            'unconfirmed_enabled_samples': sum(not s['active'] for s in known),
            'unknown_samples': sum('active' not in s for s in samples),
            'longest_unconfirmed_sample_sequence_seconds': longest * interval,
            'peak_panel_rss_kib': max(panels) if panels else None,
            'peak_xray_rss_kib': max(xrays) if xrays else None,
            'panel_rss_change_kib': panels[-1] - panels[0] if panels else None,
            'xray_rss_change_kib': xrays[-1] - xrays[0] if xrays else None}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--minutes', type=int, default=60)
    parser.add_argument('--interval', type=int, default=15)
    parser.add_argument('--user', default='admin')
    parser.add_argument('--url', default='http://192.168.50.1:8088')
    parser.add_argument('--output', type=Path, default=Path('darkls-router-soak.json'))
    args = parser.parse_args()
    if not 1 <= args.minutes <= 180 or not 15 <= args.interval <= 120:
        parser.error('minutes: 1..180; interval: 15..120 seconds')
    url = urllib.parse.urlsplit(args.url)
    # Router's own Host/Origin contract remains authoritative; do not send the
    # panel password to an arbitrary URL supplied accidentally on the command line.
    if url.scheme != 'http' or url.netloc != '192.168.50.1:8088' or url.path not in ('', '/') or url.query or url.fragment:
        parser.error('supported panel address: http://192.168.50.1:8088')
    origin = 'http://192.168.50.1:8088'
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ''

    def request(path, payload=None):
        headers = {'Origin': origin, 'X-CSRF-Token': csrf}
        body = None if payload is None else json.dumps(payload).encode()
        if body is not None:
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(origin + '/api/' + path, body, headers)
        with opener.open(req, timeout=10) as response:
            return json.loads(response.read(512 * 1024))

    password = getpass.getpass('Panel password (not SSH): ')
    try:
        request('login', {'user': args.user, 'password': password})
    finally:
        password = ''
    csrf = request('session')['csrf']
    samples = []
    started = time.monotonic()
    interrupted = False
    try:
        while time.monotonic() - started < args.minutes * 60:
            tick = time.monotonic()
            sample = {'elapsed_seconds': round(tick - started, 2)}
            try:
                status = request('status')
                for field in ('active', 'disabled', 'xray', 'dns', 'nat', 'udp', 'probe_fresh'):
                    if isinstance(status.get(field), bool):
                        sample[field] = status[field]
                probe = status.get('probe') or {}
                for field in ('recoveries', 'failures'):
                    if isinstance(probe.get(field), (int, float)):
                        sample[field] = probe[field]
                metrics = request('metrics')
                for process in ('panel', 'xray'):
                    rss = (metrics.get('processes', {}).get(process) or {}).get('rss_kib')
                    if isinstance(rss, (int, float)):
                        sample[process + '_rss_kib'] = rss
                available = metrics.get('memory_available_kib')
                if isinstance(available, (int, float)):
                    sample['memory_available_kib'] = available
            except (OSError, ValueError, KeyError):
                sample['observation_error'] = True
            samples.append(sample)
            print(f"Sample {len(samples)}; HTTPS confirmed: {sample.get('active', 'unknown')}", flush=True)
            time.sleep(max(0, min(args.interval - (time.monotonic() - tick), args.minutes * 60 - (time.monotonic() - started))))
    except KeyboardInterrupt:
        interrupted = True
    finally:
        report = {'scope': 'Observed production panel state; traffic is generated by real devices. Sampling does not measure exact outage duration.',
                  'interrupted': interrupted, 'elapsed_seconds': round(time.monotonic() - started, 2),
                  'interval_seconds': args.interval, 'summary': summarize(samples, args.interval), 'samples': samples}
        try:
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
        finally:
            try:
                request('logout', {})
            except (OSError, ValueError):
                pass
    print(args.output.resolve())


if __name__ == '__main__':
    main()
