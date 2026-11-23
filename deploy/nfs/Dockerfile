# Build with registry.fedoraproject.org, which remains reachable in the
# isolated lab even when Docker Hub is not.
FROM registry.fedoraproject.org/fedora:44

RUN dnf -y install nfs-ganesha nfs-ganesha-vfs rpcbind iproute procps-ng util-linux \
    && dnf clean all

COPY entrypoint.sh /usr/local/bin/ganesha-entrypoint.sh
COPY ganesha.conf /etc/ganesha/ganesha.conf
RUN chmod 0755 /usr/local/bin/ganesha-entrypoint.sh

EXPOSE 2049
ENTRYPOINT ["/usr/local/bin/ganesha-entrypoint.sh"]
