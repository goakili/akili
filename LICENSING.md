# Licensing

Akili is **open core**, split across three licenses.

## Akili Community — AGPL-3.0-or-later

The Akili control plane (`server/`), the agent (`agent/`) and the web UI (`web/`) are free and open
source under the **GNU Affero General Public License, version 3 or (at your option) any later
version**. See [LICENSE](LICENSE). This covers everything except the wire contract and the
Enterprise Edition files described below, including everything compiled into a build without the
`enterprise` tag.

The Community edition is complete on its own. Signed policies, approvals, the audit chain, the kill
switch, OIDC single sign-on, SIEM streaming, Vault Transit encryption, multiple replicas and agent
mTLS are all part of it, and stay part of it.

The AGPL has a **network clause** (section 13): if you run a modified Akili and let users interact
with it over a network, you must offer those users the complete corresponding source of your
modified version, also under the AGPL. Running Akili unmodified, or for your own internal use,
imposes no obligation beyond the usual AGPL terms. If that does not fit your use, a commercial
license is available.

## The wire contract (`proto/`) — Apache-2.0

[`proto`](proto) is licensed under the **Apache License, Version 2.0** (see
[`proto/LICENSE`](proto/LICENSE)), so other clients can implement the protocol without taking on
the AGPL. Nothing in `proto/` imports the server or the agent.

## Akili Enterprise — Commercial License

Enterprise features are available under the commercial **Akili Enterprise License**. The
Enterprise files are the sources under [`server/internal/enterprise/`](server/internal/enterprise)
that are built with the `enterprise` build tag. They are not AGPL; the terms are in
[`server/internal/enterprise/LICENSE.md`](server/internal/enterprise/LICENSE.md).

Official releases are built with the `enterprise` tag. Without a license key the Enterprise code
stays inactive and the binary runs as the Community edition, so the same binary and image serve
both editions. A license key is an offline, signed token, verified against a public key built into
the binary, so it works air-gapped.

A lapsed license never switches a feature off and never weakens a control: Enterprise features keep
running, and only their settings become read-only until the license is renewed.

## Dual licensing

Jonas Kaninda holds the copyright to the Akili core and offers it under **both** the AGPL (to
everyone) and a commercial license (to those who cannot or do not wish to comply with the AGPL). The
copyright holder is not bound by the AGPL for their own code, which is what allows combining the
AGPL core with the proprietary Enterprise Edition in one binary. To keep this possible as the
project takes outside contributions, contributions to the core need a Contributor License
Agreement.

## Per-file markers (SPDX)

Every source file declares its license with an [SPDX](https://spdx.dev) identifier:

- Community: `SPDX-License-Identifier: AGPL-3.0-or-later`
- The wire contract (`proto/`): `SPDX-License-Identifier: Apache-2.0`
- Enterprise: `SPDX-License-Identifier: LicenseRef-Akili-Enterprise`

Only the files built with the `enterprise` tag carry `LicenseRef-Akili-Enterprise`. The Community
stub (`ce_stub.go`), the shared interface (`enterprise.go`) and the license-token package
(`server/internal/enterprise/license/`) are AGPL-3.0-or-later.
