FROM alpine:3.14.3

# we unlock the root user for sshd
# https://github.com/alpinelinux/docker-alpine/issues/28#issuecomment-510510532
# https://github.com/alpinelinux/docker-alpine/issues/28#issuecomment-659551571
RUN apk add --no-cache rsync openssh openssh-server-pam tini && \
    ssh-keygen -A && \
    sed -i -e 's/^root:!:/root:*:/' /etc/shadow

COPY sshd_config /etc/ssh/sshd_config

EXPOSE 22

ENTRYPOINT ["tini", "--"]
CMD ["/usr/sbin/sshd", "-D", "-e", "-f", "/etc/ssh/sshd_config"]
