---
name: Incident triage
description: General first response to an alert or a report of degradation.
---
1. Establish the symptom from the alert or request, then gather facts before theories: `host_info`, `disk_usage`, `process_list`, `service_status` for the affected service, recent `journal_logs`, `docker_ps`.
2. Check recent changes: package updates (`package_updates` shows pending ones; logs show applied ones), configuration edits, deploys.
3. Classify the impact: one host or many, degraded or down, data at risk or not.
4. Fix only with `change_run` and only when the cause is clear; otherwise stop and report what you found and what a human should look at next.
5. The report is the deliverable: timeline, impact, evidence, cause (or best hypothesis with confidence), actions taken, follow-ups.
