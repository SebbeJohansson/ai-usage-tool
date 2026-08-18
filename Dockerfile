# Build stage
FROM golang:1.25 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/aiusage ./cmd/aiusage

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/aiusage /usr/local/bin/aiusage

# State file lives here; mount a volume at /app/data to persist alert state
# (which thresholds were already notified) across container restarts.
ENV STATE_FILE=/app/data/state.json

ENTRYPOINT ["aiusage"]
