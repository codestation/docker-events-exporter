FROM golang:1.20-alpine as builder

ARG CI_COMMIT_TAG
ARG GOPROXY
ENV GOPROXY=${GOPROXY}

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum /src/
RUN go mod download
COPY . /src/

RUN set -ex; \
    CGO_ENABLED=0 go build -o release/docker-events-exporter \
    -trimpath \
    -ldflags "-w -s \
    -X main.Tag=${CI_COMMIT_TAG}"

FROM alpine:3.17
LABEL maintainer="codestation <codestation@megpoid.dev>"

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /src/release/docker-events-exporter /bin/docker-events-exporter

ENTRYPOINT ["/bin/docker-events-exporter"]
