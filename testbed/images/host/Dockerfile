# Host image: load generators, metric tools and sshd (controller connects over the mgmt network).
FROM ubuntu:26.04
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        iperf3 netperf sockperf sysstat iproute2 ethtool iputils-ping traceroute procps \
        openssh-server ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /run/sshd /root/.ssh \
    # authorized_keys is bind-mounted from the controller user, so StrictModes would reject it
    && printf 'PermitRootLogin prohibit-password\nPasswordAuthentication no\nStrictModes no\n' \
        > /etc/ssh/sshd_config.d/10-netbench.conf
CMD ["/usr/sbin/sshd", "-D", "-e"]
