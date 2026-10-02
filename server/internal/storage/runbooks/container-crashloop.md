---
name: Container crash loop
description: A container keeps restarting or exits unexpectedly.
---
1. `docker_ps` with all to see restart counts and exit status; `docker_logs` of the failing container, reading the output just before each exit.
2. Common causes: missing configuration or secrets, a dependency not reachable (`net_probe`), out of memory (`host_info`, `journal_logs` for oom-killer), a full disk (`disk_usage`), a bad image version.
3. If a restart can help (a dependency was down and is back), propose `docker_restart` with `change_run`, verifying with `docker_ps` and the service's port with `net_probe`.
4. Do not change images or compose files unless asked; report the evidence instead.
