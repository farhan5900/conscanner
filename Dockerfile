FROM golang:1.16.2-stretch

RUN apt-get update && \
    apt-get install -y --no-install-recommends gettext-base apt-transport-https ca-certificates curl gnupg2 software-properties-common

RUN curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin v0.7.0
RUN rm -rf /var/lib/apt/lists/*

COPY ./main.go /conscanner/
COPY ./go.mod /conscanner/
COPY ./go.sum /conscanner/

WORKDIR /conscanner
ENV GO111MODULE=on
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o conscanner
RUN cp ./conscanner /usr/local/bin/conscanner
