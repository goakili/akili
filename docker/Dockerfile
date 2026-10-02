# Akili control plane: UI + API in one binary, plus the agent binaries it serves to install-agent.sh.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p ../server/internal/web/dist && npm run build

FROM golang:1.27-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.work go.work.sum ./
COPY proto/ proto/
COPY agent/go.mod agent/go.sum agent/
COPY server/go.mod server/go.sum server/
RUN cd server && go mod download
COPY server/ server/
COPY --from=web /src/server/internal/web/dist server/internal/web/dist
RUN cd server && CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/goakili/akili/server/internal/config.Version=${VERSION}" \
    -o /out/akili ./cmd/akili

# Agent binaries for every supported architecture, whatever the image's own: hosts download them
# from the control plane, so the agent always matches it. Cross-compiled on the build machine.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS agents
ARG VERSION=dev
WORKDIR /src
COPY go.work go.work.sum ./
COPY proto/ proto/
COPY server/go.mod server/go.sum server/
COPY agent/ agent/
WORKDIR /src/agent
RUN for arch in amd64 arm64; do \
      CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" \
        -o /out/agent/akili-agent-linux-$arch ./cmd/akili-agent || exit 1; \
    done

# The Miabi CLI, for the Miabi MCP preset (`miabi mcp`). Cross-compiled, so arm64 builds natively.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS miabi
ARG MIABI_CLI_VERSION=v0.17.0
ARG TARGETOS TARGETARCH
WORKDIR /src
RUN go mod init build && go get github.com/miabi-io/cli@${MIABI_CLI_VERSION} && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
    -ldflags "-s -w -X github.com/miabi-io/cli/internal/cmd.version=${MIABI_CLI_VERSION}" \
    -o /out/miabi github.com/miabi-io/cli

FROM alpine:3
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 akili
COPY --from=build /out/akili /usr/local/bin/akili
COPY --from=miabi /out/miabi /usr/local/bin/miabi
COPY --from=agents /out/agent/ /usr/local/share/akili/agent/
ENV AKILI_AGENT_DOWNLOADS_DIR=/usr/local/share/akili/agent
USER 10001
EXPOSE 8080
ENTRYPOINT ["akili"]
CMD ["server"]
