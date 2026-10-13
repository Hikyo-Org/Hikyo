# syntax=docker/dockerfile:1
FROM python:3.13.12-alpine3.23@sha256:bb1f2fdb1065c85468775c9d680dcd344f6442a2d1181ef7916b60a623f11d40 AS source

ADD --checksum=sha256:f5c6cee788d399064ec3dd482af1097bd8d1c752571d419bea40f0f2693ecc68 https://codeload.github.com/OpenAEV-Platform/injectors/tar.gz/e5306841e1b8593250c67a9b25e79355815d05bd /tmp/injectors.tar.gz
RUN mkdir /source && tar -xzf /tmp/injectors.tar.gz --strip-components=1 -C /source

FROM python:3.13.12-alpine3.23@sha256:bb1f2fdb1065c85468775c9d680dcd344f6442a2d1181ef7916b60a623f11d40
LABEL org.opencontainers.image.source="https://github.com/OpenAEV-Platform/injectors" \
      org.opencontainers.image.revision="e5306841e1b8593250c67a9b25e79355815d05bd" \
      org.opencontainers.image.version="3.261005.0-arm64" \
      org.opencontainers.image.description="Pinned HTTP injector for disposable Hikyo lab"

ENV PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONPATH=/opt/injector:/opt/injector_common \
    HOME=/tmp
COPY requirements.lock /tmp/requirements.lock
RUN pip install --no-cache-dir --only-binary=:all: --require-hashes -r /tmp/requirements.lock && \
    rm /tmp/requirements.lock
COPY --from=source /source/injector_common/injector_common /opt/injector_common/injector_common
COPY --from=source /source/http-query/http_query /opt/injector/http_query
COPY --from=source /source/LICENSE /opt/injector/LICENSE
WORKDIR /opt/injector
USER 10001:10001
CMD ["python", "-m", "http_query.openaev_http"]
