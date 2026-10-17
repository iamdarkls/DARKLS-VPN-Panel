# Memory microbenchmark

Linux amd64, Go 1.27.2. Three server runs per build; each handles 1200 authenticated GET requests across state/export/backups. The baseline initializes the 1.0.2 catalog; the optimized build initializes the larger 1.0.3 catalog. There is no simulated VPN traffic. This is not router idle RSS.

Build the two Linux host binaries from their respective release sources using identical build flags. Then:

    cc -O2 -o bench/rss-supervisor bench/rss-supervisor.c
    DARKLS_BASELINE_BINARY=/absolute/path/to/baseline DARKLS_OPTIMIZED_BINARY=/absolute/path/to/optimized python3 bench/memory.py

The supervisor forks the Go server from a small C process and reports that child's wait4 ru_maxrss in KiB. This avoids counting a Python parent's pre-exec RSS as the child's peak. Results are written to bench/results.json. The benchmark temporarily uses loopback port 18089 and /tmp/darkls-panel.pid; run away from a live router installation.

Median measured peak: 14396 KiB baseline, 12832 KiB optimized (~10.9% lower). Actual ARM router RAM must be measured separately; low idle usage may change little.
