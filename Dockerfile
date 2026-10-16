# ---- build ----
FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary; the SQL migrations are embedded (migrations/embed.go).
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/medsend-api .

# ---- run ----
FROM alpine:3.22
# CA certificates for MinIO/OSRM over HTTPS; tzdata so report dates follow TZ (e.g. Asia/Jakarta).
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/medsend-api /app/medsend-api

ENV APP_ENV=production APP_PORT=8080 TZ=Asia/Jakarta
EXPOSE 8080
CMD ["/app/medsend-api"]
