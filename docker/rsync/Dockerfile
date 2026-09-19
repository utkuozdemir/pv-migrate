FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

RUN apk add --no-cache rsync openssh && \
    addgroup -g 10000 pvmigrate && \
    adduser -D -u 10000 -G pvmigrate -h /home/pvmigrate pvmigrate && \
    mkdir -p /home/pvmigrate/.ssh && \
    chown -R pvmigrate:pvmigrate /home/pvmigrate && \
    chmod 700 /home/pvmigrate/.ssh
