# syntax=docker/dockerfile:1

# --- build stage -------------------------------------------------------------
ARG GO_VERSION=1.23
FROM golang:${GO_VERSION} AS build
WORKDIR /src

# Deps first for layer caching — this layer only busts when the pin changes (a
# Temporal upgrade). go.mod + go.sum are committed, so builds are reproducible.
COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
# Static, stripped binary so it runs on a distroless base.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/temporal-server .

# --- runtime stage -----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/temporal-server /usr/local/bin/temporal-server

# Config is supplied at deploy time — <configDir>/<env>.yaml, mounted here. Not
# baked in, so the same image serves every env. config.Load reads docker.yaml.
ENV TEMPORAL_CONFIG_DIR=/etc/temporal/config \
    TEMPORAL_ENVIRONMENT=docker

ENTRYPOINT ["/usr/local/bin/temporal-server"]
