# syntax=docker/dockerfile:1
# Momus — multi-stage Docker build

# Stage 1: Build
FROM rust:1.88-slim-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config libssl-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY . .

# Build the CLI binary (release, statically linked)
RUN cargo build --release -p momus-cli && \
    cp target/release/momus /usr/local/bin/momus && \
    strip /usr/local/bin/momus

# Stage 2: Runtime
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/momus /usr/local/bin/momus

ENTRYPOINT ["momus"]
CMD ["--help"]
