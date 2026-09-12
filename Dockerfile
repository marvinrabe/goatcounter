# syntax=docker/dockerfile:1-labs
# Use -labs for copy --exclude

### Build frontend assets
from docker.io/node:24-alpine as assets
workdir /goatcounter
copy package.json package-lock.json vite.config.js ./
copy assets ./assets
run npm ci && npm run build

### Build GoatCounter
from docker.io/golang:1.27 as build
workdir /goatcounter
copy --exclude=goatcounter-data --exclude=node_modules --exclude=public --exclude=Dockerfile . /goatcounter
copy --from=assets /goatcounter/public ./public
env CGO_ENABLED=1
env GOTOOLCHAIN=auto
run go build -trimpath -ldflags='-s -w -extldflags=-static' \
	-tags='osusergo,netgo,sqlite_omit_load_extension' \
	./cmd/goatcounter

# The final image is "from scratch", which has no shell to create the user and
# data directory with, so assemble that filesystem here instead. copy --from
# preserves ownership, so the data directory arrives writable.
run <<EOF
	set -euC

	mkdir -p /rootfs/home/goatcounter/goatcounter-data /rootfs/etc /rootfs/tmp

	echo 'goatcounter:x:1000:1000::/home/goatcounter:/sbin/nologin' > /rootfs/etc/passwd
	echo 'goatcounter:x:1000:'                                      > /rootfs/etc/group

	# Only needed for -geodb=maxmind:..., which fetches over HTTPS.
	cp /etc/ssl/certs/ca-certificates.crt /rootfs/etc/

	chmod 1777 /rootfs/tmp
	chown -R 1000:1000 /rootfs/home/goatcounter
EOF

### Build container
# The binary is fully static (CGO with -extldflags=-static, plus the osusergo
# and netgo tags) and embeds its own tzdata, so there is nothing left for a base
# image to provide.
from scratch
copy --from=build /rootfs/ /
copy --from=build /goatcounter/goatcounter /bin/goatcounter

env        SSL_CERT_FILE=/etc/ca-certificates.crt
expose     8080
healthcheck cmd ["/bin/goatcounter", "healthcheck"]
workdir    /home/goatcounter
user       1000:1000
volume     ["/home/goatcounter/goatcounter-data"]
entrypoint ["/bin/goatcounter"]
cmd        ["serve", "-automigrate"]
