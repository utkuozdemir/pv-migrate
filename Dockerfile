FROM scratch
COPY --from=alpine:3.23.3 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ARG TARGETPLATFORM=linux/amd64
COPY ${TARGETPLATFORM}/pv-migrate /pv-migrate
ENTRYPOINT ["/pv-migrate"]
