# syntax=docker/dockerfile:1
# Multi-stage build → static Go binary on distroless (Linux-only, non-root).
ARG GO_VERSION=1.26

FROM golang:${GO_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO off → fully static binary (pgx/minio-go/otel are all pure Go).
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/baasparse ./cmd/baasparse

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=build /out/baasparse /app/baasparse
# schema.sql ships alongside the binary; setupdb finds it exe-relative.
COPY .db/schema.sql /app/schema.sql
# Give os.UserHomeDir() a defined target (the config file is optional in-container
# — config comes from env — but this keeps HOME-derived paths sane).
ENV HOME=/home/nonroot
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/baasparse"]
