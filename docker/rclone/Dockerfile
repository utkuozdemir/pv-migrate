FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

# Keep this UID/GID in sync with nonRootUID in internal/bucketstorage.
RUN apk add --no-cache rclone && \
    addgroup -g 10000 pvmigrate && \
    adduser -D -u 10000 -G pvmigrate -h /home/pvmigrate pvmigrate && \
    chown -R pvmigrate:pvmigrate /home/pvmigrate
