FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/commandcode-proxy ./cmd/commandcode-proxy

FROM scratch
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/commandcode-proxy /app/commandcode-proxy
COPY configs/ /app/configs/

USER 65532:65532
ENV PORT=3050 HOST=0.0.0.0
EXPOSE 3050
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/app/commandcode-proxy", "-healthcheck"]
ENTRYPOINT ["/app/commandcode-proxy"]
