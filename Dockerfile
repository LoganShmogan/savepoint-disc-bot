FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/bot /bot
# The named volume copies this dir's nonroot ownership on first mount.
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV DB_PATH=/data/hub.db CONFIG_PATH=/config/projects.yaml API_ADDR=:8080
ENTRYPOINT ["/bot"]
