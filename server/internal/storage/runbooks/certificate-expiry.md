---
name: Certificate expiry
description: A TLS certificate is expiring, expired or not trusted.
---
1. `cert_check` the host (and port) to see the served chain, names, issuer and days left, and whether the chain verifies.
2. Find where the certificate comes from: ACME client (certbot, Caddy, Traefik), a reverse proxy config in `/etc`, or a mounted secret. Check the renewal job's logs with `journal_logs`.
3. If an automated renewal failed, find why (DNS, HTTP-01 reachability with `net_probe`, rate limits, permissions) and propose the fix with `change_run`; verify with `cert_check` expecting "status:   ok".
4. Never generate or copy private keys through chat or tool output.
5. Report the expiry date, cause, fix, and whether other hosts use the same certificate.
