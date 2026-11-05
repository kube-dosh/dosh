FROM gcr.io/distroless/static-debian12:nonroot

COPY bin/kube-fault-proxy /usr/local/bin/kube-fault-proxy

USER 65532:65532
EXPOSE 8443 8081
ENTRYPOINT ["/usr/local/bin/kube-fault-proxy"]
