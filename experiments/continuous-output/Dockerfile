ARG RESTREAMER_IMAGE=ghcr.io/kreuzhofer/restreamer:sha-85f678e36daa8b28e104cfb5330d717d4d22688d
# Build only the loopback capture helper; production code is unchanged.
FROM golang:1.27.1-alpine AS captures
WORKDIR /src
COPY go.mod go.sum ./
COPY internal/rtmp ./internal/rtmp
COPY experiments/continuous-output/sinks ./experiments/continuous-output/sinks
RUN GOMAXPROCS=2 CGO_ENABLED=0 go build -trimpath -o /out/rtmp-sinks ./experiments/continuous-output/sinks

FROM ${RESTREAMER_IMAGE}
USER root
RUN apk add --no-cache python3
COPY experiments/continuous-output/benchmark.py experiments/continuous-output/playout.py experiments/continuous-output/delivery.py experiments/continuous-output/prepare_markers.py /opt/continuous-benchmark/
COPY --from=captures /out/rtmp-sinks /opt/continuous-benchmark/rtmp-sinks
USER 65532:65532
HEALTHCHECK NONE
ENTRYPOINT ["python3", "/opt/continuous-benchmark/benchmark.py"]
CMD []
