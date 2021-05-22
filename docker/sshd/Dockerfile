FROM alpine:3

RUN apk add --no-cache rsync openssh openssh-server-pam tini && ssh-keygen -A

COPY sshd_config /etc/ssh/sshd_config

ENTRYPOINT ["tini", "--"]
CMD ["/usr/sbin/sshd", "-D", "-e", "-f", "/etc/ssh/sshd_config"]
