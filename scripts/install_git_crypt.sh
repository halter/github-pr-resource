#!/bin/sh

set -eu

_main() {
	apk add --no-cache --repository=https://dl-cdn.alpinelinux.org/alpine/v3.23/community git-crypt
}

_main "$@"
