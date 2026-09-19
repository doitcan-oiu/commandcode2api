FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build
RUN mkdir -p /out/licenses && \
    find node_modules -type f \( -iname 'license' -o -iname 'license.*' -o -iname 'license-*' -o -iname 'copying*' -o -iname 'notice*' \) \
    -exec sh -c 'for notice do printf "\n===== %s =====\n" "$notice"; cat "$notice"; done' sh {} + \
    > /out/licenses/FRONTEND_DEPENDENCIES.txt

FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/

ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w" -o /out/commandcode-proxy ./cmd/commandcode-proxy
RUN mkdir -p /out/data /out/tmp /out/licenses && \
    chown 65532:65532 /out/data && chmod 1777 /out/tmp && \
    cp /usr/local/go/LICENSE /out/licenses/GO_LICENSE.txt && \
    find /go/pkg/mod -type f \( -iname 'license' -o -iname 'license.*' -o -iname 'license-*' -o -iname 'copying*' -o -iname 'notice*' \) \
    -exec sh -c 'for notice do printf "\n===== %s =====\n" "$notice"; cat "$notice"; done' sh {} + \
    > /out/licenses/GO_DEPENDENCIES.txt

FROM scratch
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/commandcode-proxy /app/commandcode-proxy
COPY configs/ /app/configs/
COPY LICENSE THIRD_PARTY_NOTICES.md /app/
COPY --from=frontend /src/web/dist/ /app/web/dist/
COPY --from=frontend /out/licenses/ /app/licenses/
COPY --from=build /out/licenses/ /app/licenses/
COPY --from=build --chown=65532:65532 /out/data/ /app/data/
COPY --from=build --chmod=1777 /out/tmp/ /tmp/

USER 65532:65532
ENV PORT=3050 HOST=0.0.0.0 CC_DATA_DIR=/app/data CC_WEB_DIR=/app/web/dist TMPDIR=/tmp
VOLUME ["/app/data"]
EXPOSE 3050
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/app/commandcode-proxy", "-healthcheck"]
ENTRYPOINT ["/app/commandcode-proxy"]
