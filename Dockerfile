# The binary is built by `make build` because the frontend is embedded into the
# Go executable. This keeps the runtime image minimal and non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY bin/dosh /usr/local/bin/dosh
USER 65532:65532
EXPOSE 32626
ENTRYPOINT ["/usr/local/bin/dosh"]
