---
name: Disk space
description: A filesystem is filling up; find what grew and free space safely.
---
1. `disk_usage` on `/` (and the mount the alert names) to see which filesystem is full and which directories are largest; drill into the largest with `disk_usage` on that path.
2. Identify the cause: logs (`/var/log`, container logs), caches, old releases, core dumps, Docker images/volumes (`docker_ps` with all), temporary files. Check `journal_logs` for a service writing unusually fast.
3. Prefer safe, reversible cleanups: rotate or truncate logs a service keeps open, remove files older than N days in caches or `/var/tmp`, prune unused Docker images. Never delete data directories (databases, uploads) or anything you cannot explain.
4. Make the cleanup with `change_run`. Verify with `disk_usage` (expect usage to drop below the alert threshold) and, if a service was involved, `service_status`. Rollback is often impossible for deletions: say so in the plan's reason and prefer moving files to a dated archive directory when space allows.
5. Report what grew, what you removed or moved, space before and after, and the fix that prevents a repeat (log rotation, retention, alert threshold).
