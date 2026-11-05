FROM gcr.io/distroless/static-debian12:nonroot

COPY bin/audit-receiver /usr/local/bin/audit-receiver

USER 65532:65532
EXPOSE 8443
ENTRYPOINT ["/usr/local/bin/audit-receiver"]
