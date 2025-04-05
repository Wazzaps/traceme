FROM ubuntu:24.04

RUN apt-get update \
    && apt-get install -y git libatomic1 nano net-tools sudo curl zstd socat \
    && mkdir -p /app/code-server \
    && curl -o /usr/bin/docker-init -L https://github.com/openSUSE/catatonit/releases/download/v0.2.1/catatonit.x86_64 \
    && chmod +x /usr/bin/docker-init \
    && curl -o /tmp/code-server.tar.gz -L \
    "https://github.com/coder/code-server/releases/download/v4.98.2/code-server-4.98.2-linux-amd64.tar.gz" \
    && tar xf /tmp/code-server.tar.gz -C /app/code-server --strip-components=1 \
    && rm /tmp/code-server.tar.gz \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /config/data /config/extensions

RUN curl -L https://go.dev/dl/go1.24.2.linux-amd64.tar.gz | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:$PATH"
RUN GOPATH=/usr/local/go go install -v github.com/go-delve/delve/cmd/dlv@latest
RUN GOPATH=/usr/local/go go install -v golang.org/x/tools/gopls@latest
RUN GOPATH=/usr/local/go go install -v honnef.co/go/tools/cmd/staticcheck@latest
COPY --from=wazzaps/traceme:latest /opt/traceme /opt/traceme
COPY build/tracebrowser /opt/traceme/bin/tracebrowser
RUN echo '[safe]\ndirectory = *' > /etc/gitconfig

EXPOSE 8080

CMD ["/opt/traceme/bin/tracebrowser"]