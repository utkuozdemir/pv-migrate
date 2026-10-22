FROM alpine:3.24.3@sha256:b3413572228e34bd1c9245f5565e6cd127005655e8a7899aef33d8e83255c9e6

RUN apk add --no-cache rsync openssh && \
    addgroup -g 10000 pvmigrate && \
    adduser -D -u 10000 -G pvmigrate -h /home/pvmigrate pvmigrate && \
    mkdir -p /home/pvmigrate/.ssh && \
    chown -R pvmigrate:pvmigrate /home/pvmigrate && \
    chmod 700 /home/pvmigrate/.ssh
