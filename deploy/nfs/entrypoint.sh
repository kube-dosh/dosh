#!/usr/bin/env bash
set -euo pipefail

export_dir="${NFS_EXPORT_DIR:-/export}"
mkdir -p "$export_dir" /var/run/ganesha /var/lib/nfs/ganesha
# Dosh runs as UID/GID 65532. Keep the export root writable without recursively
# walking existing data on every NFS pod restart.
chown 65532:65532 "$export_dir"
chmod 0770 "$export_dir"
rpcbind -w || true
exec /usr/bin/ganesha.nfsd -F -L STDOUT -f /etc/ganesha/ganesha.conf
