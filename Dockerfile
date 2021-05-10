FROM golang:1.16.2-stretch

RUN apt-get update && \
    apt-get install -y --no-install-recommends gettext-base apt-transport-https ca-certificates curl gnupg2 software-properties-common

RUN curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin v0.7.0
RUN rm -rf /var/lib/apt/lists/*

WORKDIR /root/
COPY conscanner /root
ENTRYPOINT ["./conscanner"]
