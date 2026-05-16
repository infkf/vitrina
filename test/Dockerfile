FROM ubuntu:24.04

RUN apt-get update -qq && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    ca-certificates curl gnupg git sudo

# Docker
RUN install -m 0755 -d /etc/apt/keyrings && \
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc && \
    chmod a+r /etc/apt/keyrings/docker.asc && \
    printf 'deb [arch=%s signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu %s stable\n' \
      "$(dpkg --print-architecture)" "$(. /etc/os-release && echo $VERSION_CODENAME)" \
      | tee /etc/apt/sources.list.d/docker.list >/dev/null && \
    apt-get update -qq && \
    apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

# Caddy
RUN curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
      | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg && \
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
      | tee /etc/apt/sources.list.d/caddy-stable.list >/dev/null && \
    apt-get update -qq && \
    apt-get install -y -qq caddy

COPY vitrina /usr/local/bin/vitrina
COPY test/run.sh /test/run.sh

RUN chmod +x /usr/local/bin/vitrina /test/run.sh

ENTRYPOINT ["/test/run.sh"]
