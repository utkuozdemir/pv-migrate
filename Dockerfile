FROM scratch
COPY --from=alpine:3.24.3@sha256:b3413572228e34bd1c9245f5565e6cd127005655e8a7899aef33d8e83255c9e6 /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/pv-migrate /pv-migrate
ENTRYPOINT ["/pv-migrate"]
