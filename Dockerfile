# syntax=docker/dockerfile:1

# ---- build stage ----------------------------------------------------------
# Pinned to a specific Go minor so a base-image bump is a deliberate commit
# rather than something that happens to you between builds.
FROM golang:1.26-bookworm AS build

WORKDIR /src

# Copy the module files alone first. Docker caches this layer on their content,
# so dependencies are only re-downloaded when go.mod or go.sum actually change,
# not on every source edit.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG SERVICE_NAME=dyson-sphere-service

# CGO_ENABLED=0 produces a static binary, which is what lets the runtime stage
# be distroless static -- no libc to link against there.
# -trimpath strips local filesystem paths out of the binary, so builds are
# reproducible and stack traces do not leak /Users/whoever.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build \
      -trimpath \
      -ldflags="-s -w \
        -X gitlab.com/navetoocool/dyson-sphere/internal/build.Version=${VERSION} \
        -X gitlab.com/navetoocool/dyson-sphere/internal/build.Commit=${COMMIT} \
        -X gitlab.com/navetoocool/dyson-sphere/internal/build.ServiceName=${SERVICE_NAME}" \
      -o /out/server ./cmd/server

# ---- runtime stage --------------------------------------------------------
# distroless static: no shell, no package manager, no busybox. If someone gets
# code execution in this container there is nothing in it to pivot with.
# The :nonroot tag runs as uid 65532 by default.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/server /server

# Documentation only -- EXPOSE publishes nothing by itself.
EXPOSE 8080

USER nonroot:nonroot

# Exec form, so the binary is PID 1 and receives SIGTERM directly. The shell
# form would wrap it in /bin/sh, which does not forward signals, and the
# graceful shutdown written in Session 1 would never run.
ENTRYPOINT ["/server"]
