#!/bin/sh
# Akili agent installer.
#
#   curl -fsSL https://akili.example.com/install-agent.sh | sudo AKILI_URL=... AKILI_JOIN_TOKEN=... sh
#
# With a self-signed or private-CA control plane, trust its CA for the download too:
#
#   curl -fsSL --cacert ca.pem https://akili.example.com/install-agent.sh | sudo AKILI_URL=... AKILI_JOIN_TOKEN=... AKILI_CA_CERT=$PWD/ca.pem sh
#
# Installs akili-agent as a hardened systemd service running as the unprivileged "akili" user,
# enrolls it with the control plane using the one-time join token, and starts it. The agent is
# downloaded from the GitHub release matching the control plane, so the host needs to reach github.com.
#
# Optional:
#   AKILI_AGENT_VERSION     the release to install, e.g. 0.0.2 (default: the control plane's version)
#   AKILI_AGENT_RELEASE_URL a mirror with the GitHub release layout, for hosts without access to
#                           github.com (default: https://github.com/goakili/akili/releases).
#                           Set AKILI_AGENT_VERSION too: "latest" is only resolved on GitHub.
#   AKILI_AGENT_WORKDIR     the agent's working directory (default: /var/lib/akili-agent/work)
#   AKILI_CA_CERT           path to a CA bundle (PEM) to trust for the control plane
#   AKILI_CA_CERT_PEM       the same CA as inline PEM, instead of a file
set -eu

fail() { echo "akili: $*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || fail "run as root (sudo)"
[ -n "${AKILI_URL:-}" ] || fail "AKILI_URL is required"
[ -n "${AKILI_JOIN_TOKEN:-}" ] || fail "AKILI_JOIN_TOKEN is required"
command -v systemctl >/dev/null 2>&1 || fail "systemd is required (use the Docker install on hosts without it)"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) fail "unsupported architecture $(uname -m)" ;;
esac

BIN=/usr/local/bin/akili-agent
STATE=/var/lib/akili-agent
WORKDIR="${AKILI_AGENT_WORKDIR:-$STATE/work}"
RELEASES="${AKILI_AGENT_RELEASE_URL:-https://github.com/goakili/akili/releases}"
RELEASES="${RELEASES%/}"
# The control plane fills in its own version when it serves this script, so the agent matches it.
VERSION="${AKILI_AGENT_VERSION:-__AKILI_AGENT_VERSION__}"
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum is required to verify the download"
command -v tar >/dev/null 2>&1 || fail "tar is required"

tmp="$(mktemp -d)"
cafile=""
trap 'rm -rf "$tmp" "$cafile"' EXIT
if [ -n "${AKILI_CA_CERT_PEM:-}" ]; then
  cafile="$(mktemp)"
  printf '%s\n' "$AKILI_CA_CERT_PEM" >"$cafile"
  AKILI_CA_CERT="$cafile"
fi
if [ -n "${AKILI_CA_CERT:-}" ]; then
  [ -r "$AKILI_CA_CERT" ] || fail "cannot read AKILI_CA_CERT $AKILI_CA_CERT"
  grep -q "BEGIN CERTIFICATE" "$AKILI_CA_CERT" || fail "AKILI_CA_CERT is not a PEM certificate"
fi

if [ "$VERSION" = latest ]; then
  # GitHub redirects /releases/latest to /releases/tag/v<version>.
  tag="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$RELEASES/latest")" || fail "cannot find the latest release at $RELEASES/latest"
  VERSION="${tag##*/}"
fi
VERSION="${VERSION#v}"
case "$VERSION" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) fail "not a release version: '$VERSION' (set AKILI_AGENT_VERSION, e.g. 0.0.2)" ;;
esac
case "$VERSION" in *[!0-9A-Za-z.-]*) fail "not a release version: '$VERSION'" ;; esac

# Downloads use the system's trusted CAs, not AKILI_CA_CERT: that CA is for the control plane only.
ARCHIVE="akili-agent_${VERSION}_linux_${ARCH}.tar.gz"
BASE="$RELEASES/download/v$VERSION"
echo "akili: downloading agent $VERSION ($ARCH)"
curl -fsSL "$BASE/$ARCHIVE" -o "$tmp/$ARCHIVE" || fail "download failed: $BASE/$ARCHIVE"
curl -fsSL "$BASE/checksums.txt" -o "$tmp/checksums.txt" || fail "download failed: $BASE/checksums.txt"
want="$(awk -v f="$ARCHIVE" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "$ARCHIVE is not listed in $BASE/checksums.txt"
[ "$(sha256sum "$tmp/$ARCHIVE" | cut -d' ' -f1)" = "$want" ] || fail "checksum mismatch for $ARCHIVE"
tar -xzf "$tmp/$ARCHIVE" -C "$tmp" akili-agent || fail "$ARCHIVE has no akili-agent binary"
install -m 0755 "$tmp/akili-agent" "$BIN"

if ! id akili >/dev/null 2>&1; then
  useradd --system --home-dir "$STATE" --shell /usr/sbin/nologin akili
fi
install -d -m 0700 -o akili -g akili "$STATE"
install -d -m 0750 -o akili -g akili "$WORKDIR"

CA_FLAG=""
if [ -n "${AKILI_CA_CERT:-}" ]; then
  install -m 0644 "$AKILI_CA_CERT" "$STATE/ca.pem"
  CA_FLAG="--ca-cert $STATE/ca.pem"
fi

echo "akili: enrolling with $AKILI_URL"
# The token is passed through the environment, not argv, so it never shows in `ps`.
env AKILI_JOIN_TOKEN="$AKILI_JOIN_TOKEN" runuser -u akili -- "$BIN" enroll \
  --url "$AKILI_URL" --state-dir "$STATE" --workdir "$WORKDIR" $CA_FLAG

cat > /etc/systemd/system/akili-agent.service <<UNIT
[Unit]
Description=Akili agent
After=network-online.target
Wants=network-online.target

[Service]
User=akili
Group=akili
ExecStart=$BIN run --state-dir $STATE
Restart=always
RestartSec=5
# Drain in-flight sessions on stop.
KillSignal=SIGTERM
TimeoutStopSec=60

# Hardening: the agent gets no privileges beyond its user; anything privileged goes through an
# explicit sudoers allowlist maintained by the operator.
NoNewPrivileges=yes
ProtectSystem=full
ProtectHome=read-only
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
PrivateTmp=yes
PrivateDevices=yes
RestrictSUIDSGID=yes
LockPersonality=yes
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=$STATE $WORKDIR

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now akili-agent
sleep 2
systemctl --no-pager --lines=5 status akili-agent || true
echo "akili: installed. Logs: journalctl -u akili-agent -f"
