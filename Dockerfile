# syntax=docker/dockerfile:1-labs
# Use -labs for copy --exclude

### Build frontend assets
from docker.io/node:24-alpine as assets
workdir /goatcounter
copy package.json package-lock.json vite.config.js ./
copy internal/web/assets ./internal/web/assets
copy internal/web/templates ./internal/web/templates
run --mount=type=cache,target=/root/.npm npm ci && npm run build

### Build GoatCounter
from docker.io/golang:1.27 as build
workdir /goatcounter
copy go.mod go.sum ./
run --mount=type=cache,target=/go/pkg/mod go mod download
copy --exclude=goatcounter-data --exclude=node_modules --exclude=internal/web/dist --exclude=Dockerfile . /goatcounter
copy --from=assets /goatcounter/internal/web/dist ./internal/web/dist
# Pure Go, including SQLite, so the binary is static without a C toolchain.
env CGO_ENABLED=0
env GOTOOLCHAIN=auto
run --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	go build -trimpath -ldflags='-s -w' ./cmd/goatcounter

# The final image is "from scratch", so assemble its user and temporary
# directories here. Persistent state belongs to the shared database service.
run <<EOF
	set -euC

	mkdir -p /rootfs/home/goatcounter /rootfs/etc /rootfs/tmp

	echo 'goatcounter:x:1000:1000::/home/goatcounter:/sbin/nologin' > /rootfs/etc/passwd
	echo 'goatcounter:x:1000:'                                      > /rootfs/etc/group

	# Required for remote libSQL, OIDC, and the bot and spam list updates.
	cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/

	chmod 1777 /rootfs/tmp
	chown -R 1000:1000 /rootfs/home/goatcounter
EOF

### Build container
# The binary is fully static (no CGO) and embeds its own tzdata, so there is
# nothing left for a base image to provide.
from scratch
copy --from=build /rootfs/ /
copy --from=build /goatcounter/goatcounter /bin/goatcounter

env        SSL_CERT_FILE=/etc/ca-certificates.crt
expose     8080
healthcheck cmd ["/bin/goatcounter", "healthcheck"]
workdir    /home/goatcounter
user       1000:1000
entrypoint ["/bin/goatcounter"]
cmd        ["serve"]
