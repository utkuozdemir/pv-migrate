FROM scratch
COPY --from=alpine:3.24.1@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/pv-migrate /pv-migrate
ENTRYPOINT ["/pv-migrate"]
