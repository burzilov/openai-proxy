#!/bin/sh
set -e

# Docker volumes are often created as root; the app runs as uid 1000 (proxy).
if [ "$(id -u)" = "0" ]; then
	mkdir -p /data
	chown -R proxy:proxy /data
	chmod 700 /data
	exec su-exec proxy /app/openai-proxy "$@"
fi

exec /app/openai-proxy "$@"
