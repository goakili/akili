---
name: Deployment rollback
description: A recent deploy broke something and the previous version should be restored.
---
1. Confirm the timeline: when the deploy happened and when errors started (`journal_logs`, `docker_logs`, `service_status`).
2. Identify the previous good version (release directory, image tag, package version) with read-only tools.
3. Propose the rollback with `change_run`: steps switch to the previous version and restart; verify with health checks (`service_status`, `net_probe`, logs free of the error); rollback of the rollback returns to the new version.
4. Report what was rolled back, the evidence it fixed the problem, and what the failing release needs.
