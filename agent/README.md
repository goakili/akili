# akili-agent

The agent of [Akili](https://github.com/goakili/akili): it runs on your servers, dials out to the
control plane over an encrypted tunnel, and runs chat and task sessions within the signed policy the
control plane sends it. It holds no LLM or forge credentials: every model call and every git push goes
through the control plane.

Install it from your control plane (Agents → Add agent). The control plane serves this binary at
`/downloads/akili-agent-linux-<arch>`, so the agent always matches it.

## Development

This module lives in the [Akili repository](https://github.com/goakili/akili) next to the control plane
(`../server`) and the wire contract (`../proto`, via the `replace` in go.mod). From the repository root:

```bash
make agent   # bin/akili-agent and the Linux binaries
make test    # unit tests of every module, including sandbox-escape cases
make vet
```

## License

[AGPL-3.0-or-later](../LICENSE). Copyright © 2026 [Jonas Kaninda](https://jkaninda.dev/)
