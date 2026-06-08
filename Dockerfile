ARG GO_VERSION=1.25

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0

RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/kvindexer ./cmd/kvindexer && \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/kvgateway ./cmd/kvgateway && \
    mkdir -p /out/data /out/tmp

FROM scratch

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build --chown=65532:65532 /out/tmp /tmp
COPY --from=build /out/kvindexer /usr/local/bin/kvindexer
COPY --from=build /out/kvgateway /usr/local/bin/kvgateway

USER 65532:65532
WORKDIR /
ENV TMPDIR=/tmp

EXPOSE 8090 8095

ENTRYPOINT ["/usr/local/bin/kvindexer"]
