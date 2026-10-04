module github.com/goakili/akili/agent

go 1.27.1

require (
	github.com/creack/pty v1.1.21
	github.com/gorilla/websocket v1.5.3
	github.com/hashicorp/yamux v0.1.2
	github.com/goakili/akili/proto v0.0.0
	github.com/jkaninda/logger v0.0.5
	github.com/jkaninda/wstunnel v0.0.2
)

require gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect

replace github.com/goakili/akili/proto => ../proto
