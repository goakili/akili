---
name: High load
description: CPU, memory or load average is high.
---
1. `host_info` for load and memory, then `process_list` sorted by cpu and by mem to find the consumers.
2. Map the top processes to services (`service_status`) or containers (`docker_ps`), and read their recent logs (`journal_logs`, `docker_logs`) for loops, retries, or traffic spikes.
3. Distinguish normal peaks (batch jobs, backups, deploys) from faults (runaway process, memory leak, crash loop).
4. Only if a single service is clearly faulty, propose a restart with `change_run` and verify load drops (`host_info`). Never kill unknown processes.
5. Report the consumer, the evidence, what you changed, and whether capacity needs to grow.
