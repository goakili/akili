---
name: Service down
description: A systemd service or container is failing or not responding.
---
1. `service_status` (or `docker_ps` with all for containers) to see the state, recent restarts and exit codes.
2. `journal_logs` for the unit since the failure (or `docker_logs`) and read the first error, not the last; check `disk_usage`, `host_info` (memory) and `net_probe` to dependencies (database, upstream APIs) for resource or network causes.
3. If a configuration change caused it (recent edits in `/etc`), show the difference before touching anything.
4. A restart is a change: propose it with `change_run` (steps: `service_restart`; verify: `service_status` expecting "active (running)" and a `net_probe` to its port). If the root cause is not fixed by a restart, stop after one attempt and report.
5. Report the cause, what you did, verification evidence, and what would prevent it.
