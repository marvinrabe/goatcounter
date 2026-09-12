#!/bin/sh
# Regenerate list.go from the Matomo referrer-spam-list.
#
# Note: uses GNU sed extensions (\t, \0); run with GNU sed on a non-Linux host.
set -eu
cd "$(dirname "$0")"

curl -s https://raw.githubusercontent.com/matomo-org/referrer-spam-list/master/spammers.txt |
sort -u |
sed 's!.*!\t"\0": {},!' |
sed -e '/^\t\/\/ %%START%%/r /dev/stdin' -e '/^\t\/\/ %%START%%/,/^\t\/\/ %%END%%/{//!d}' list.go |
gofmt > x && mv -f x list.go
