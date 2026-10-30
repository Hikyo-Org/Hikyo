FROM debian:13-slim@sha256:a29215f6a35e51e22adffa17f89e9d2ef06214e64a2bad10d765c46aea49f11f
RUN apt-get update && apt-get install -y --no-install-recommends python3 openssh-server openssh-client sudo acl sqlite3 curl ca-certificates coreutils procps && rm -rf /var/lib/apt/lists/*
CMD ["sleep", "infinity"]
