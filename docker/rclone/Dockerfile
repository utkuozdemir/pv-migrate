FROM alpine:3.23.3

RUN apk add --no-cache rclone && \
    addgroup -g 10000 pvmigrate && \
    adduser -D -u 10000 -G pvmigrate -h /home/pvmigrate pvmigrate && \
    chown -R pvmigrate:pvmigrate /home/pvmigrate
