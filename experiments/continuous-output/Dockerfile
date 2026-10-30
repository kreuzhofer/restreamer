# Pin the released media environment used for the initial capacity comparison.
ARG RESTREAMER_IMAGE=ghcr.io/kreuzhofer/restreamer:sha-85f678e36daa8b28e104cfb5330d717d4d22688d
FROM ${RESTREAMER_IMAGE}
USER root
RUN apk add --no-cache python3
COPY experiments/continuous-output/benchmark.py /opt/continuous-benchmark/benchmark.py
USER 65532:65532
HEALTHCHECK NONE
ENTRYPOINT ["python3", "/opt/continuous-benchmark/benchmark.py"]
CMD []
