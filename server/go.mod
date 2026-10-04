module github.com/goakili/akili/server

go 1.26.0

require (
	github.com/anthropics/anthropic-sdk-go v1.76.0
	github.com/boombuler/barcode v1.1.0
	github.com/coreos/go-oidc/v3 v3.21.0
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/goakili/akili/proto v0.0.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/goposta/posta-go v0.1.0
	github.com/gorilla/websocket v1.5.3
	github.com/hashicorp/yamux v0.1.2
	github.com/jkaninda/go-utils v0.1.4
	github.com/jkaninda/logger v0.0.5
	github.com/jkaninda/okapi v1.0.0
	github.com/jkaninda/wstunnel v0.0.2
	github.com/joho/godotenv v1.5.1
	github.com/redis/go-redis/v9 v9.22.0
	github.com/robfig/cron/v3 v3.0.1
	golang.org/x/crypto v0.57.0
	golang.org/x/oauth2 v0.37.0
	golang.org/x/sys v0.48.0
	gorm.io/driver/postgres v1.6.3
	gorm.io/driver/sqlite v1.6.0
	gorm.io/gorm v1.31.2
)

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/getkin/kin-openapi v0.145.0 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/jkaninda/njia v0.0.3 // indirect
	github.com/mattn/go-sqlite3 v1.14.22 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/goakili/akili/proto => ../proto
