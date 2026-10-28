#!/bin/sh
# The config generated from the templates goes to /tmp (the only writable
# path with a read-only root filesystem); /etc/nginx/conf.d links there.
set -e
mkdir -p /tmp/conf.d
