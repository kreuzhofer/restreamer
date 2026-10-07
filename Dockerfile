# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS=linux
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/restreamer ./cmd/restreamer

FROM scratch
LABEL org.opencontainers.image.source="https://github.com/kreuzhofer/restreamer"
COPY THIRD_PARTY_NOTICES /THIRD_PARTY_NOTICES
COPY config.example.json /etc/restreamer/config.json
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/restreamer /restreamer
USER 65532:65532
EXPOSE 1935 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 CMD ["/restreamer", "healthcheck"]
ENTRYPOINT ["/restreamer"]
CMD ["-config", "/etc/restreamer/config.json"]
