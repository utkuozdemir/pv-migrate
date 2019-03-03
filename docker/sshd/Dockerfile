FROM sickp/alpine-sshd:7.5-r2

RUN passwd -d root && apk add --no-cache rsync
COPY pv-migrate.pub /root/.ssh/authorized_keys
