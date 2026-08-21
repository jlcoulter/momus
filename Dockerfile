# syntax=docker/dockerfile:1

# Momus is a Go CLI (no cgo). Build a static binary in a builder stage, then
# copy it into a minimal, non-root runtime image.

ARG VERSION=0.0.0

FROM golang:1.26 AS build
ARG VERSION
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/momus ./cmd/momus

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/momus /usr/local/bin/momus
# Momus writes output/dependency directories (e.g. --output, .momus/) to the
# current working directory, so default to a writable working directory.
WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/momus"]
