# syntax=docker/dockerfile:1@sha256:34b128e419449565adc5ed7f487a6f503a73f1077012cfed86354c731338c44f

FROM --platform=linux/amd64 docker.io/library/golang:1.27.0-alpine@sha256:c0ef102fd47cc7cfb3db3e93c4830f500307e37dad1dca44a3795e783cb0bf58 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY scripts/build-linux.sh ./scripts/build-linux.sh

ARG VERSION=dev
ARG SOURCE_DATE_EPOCH=0
RUN OUTPUT=/out/shadowdns \
    VERSION="${VERSION}" \
    SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH}" \
    ./scripts/build-linux.sh

FROM --platform=linux/amd64 gcr.io/distroless/static-debian13:nonroot@sha256:23795be0fe67b7d47d1ee62b19c7db750152db627d5bbfa31307e892a7575bec

ARG SOURCE=https://example.org/shadowdns
ARG REVISION=unknown
ARG VERSION=dev
ARG CREATED=1970-01-01T00:00:00Z

LABEL org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${CREATED}"

COPY --from=build /out/shadowdns /usr/local/bin/shadowdns

USER 65532:65532

EXPOSE 5353/udp 5353/tcp 9153/tcp

ENTRYPOINT ["/usr/local/bin/shadowdns"]
CMD ["--named-conf", "/etc/shadowdns/named.conf", "--config", "/etc/shadowdns/shadowdns.yaml", "--listen", "0.0.0.0:5353", "--no-color"]
