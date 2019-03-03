FROM alpine:3.9

COPY pv-migrate /root/.ssh/id_rsa
COPY ssh-config /root/.ssh/config

RUN apk add --no-cache rsync openssh && chmod 600 /root/.ssh/id_rsa
