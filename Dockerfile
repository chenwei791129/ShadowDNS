# syntax=docker/dockerfile:1@sha256:34b128e419449565adc5ed7f487a6f503a73f1077012cfed86354c731338c44f

FROM --platform=linux/amd64 docker.io/library/golang:1.27.3-alpine@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build

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

FROM --platform=linux/amd64 gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

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
