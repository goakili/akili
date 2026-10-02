# Deployment examples

Docker Compose files for running Akili with the published images (`jkaninda/akili` and
`jkaninda/akili-agent`, also on `ghcr.io/goakili`).

| File | What it runs |
|---|---|
| [`compose.yml`](compose.yml) | One control plane with PostgreSQL and Redis. An optional local agent under the `agent` profile. |
| [`compose-agent.yml`](compose-agent.yml) | An agent alone, on another host, connecting to your control plane. |

## Run

```bash
cd examples
cp .env.example .env     # set AKILI_PUBLIC_URL, AKILI_JWT_SECRET, AKILI_ENCRYPTION_KEY, POSTGRES_PASSWORD
docker compose up -d
docker compose logs akili | grep password   # the generated owner password, if you left it empty
```

Open `AKILI_PUBLIC_URL`, sign in, then add an agent under **Agents → Add agent**. The control plane
serves the agent binary, so the install command it shows always installs a matching agent.

For a quick local test over plain HTTP, set `AKILI_PUBLIC_URL=http://localhost:8080` and
`AKILI_COOKIE_SECURE=false`.

## Production notes

- **TLS.** Serve Akili over HTTPS: terminate TLS in a reverse proxy, or set `AKILI_TLS_CERT_FILE`
  and `AKILI_TLS_KEY_FILE` to have Akili serve it itself.
- **Proxy timeouts.** Agent tunnels, the browser terminal and live updates are long-lived. Whatever
  sits in front of Akili needs read, write and idle timeouts of at least 900 s (3600 s recommended),
  WebSocket upgrades passed through and response buffering off. The main README has an nginx example
  ([Reverse proxy and load balancer](../README.md#reverse-proxy-and-load-balancer)).
- **Secrets.** Keep `AKILI_ENCRYPTION_KEY` safe and backed up. Pin `AKILI_VERSION`.
- **Backups.** All state is in PostgreSQL. Redis holds only transient events and presence.

## Miabi

Akili is also packaged for the Miabi marketplace, which provisions PostgreSQL, Redis and the route
for you: [deploy Akili on Miabi](https://marketplace.miabi.io/templates/akili).
