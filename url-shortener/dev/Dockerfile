FROM golang:1.27-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

FROM base AS development
RUN go tool air -v
COPY . .
EXPOSE 8080
CMD ["go", "tool", "air", "-c", ".air.toml"]

FROM base AS build
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/app ./cmd

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
WORKDIR /app
COPY --from=build /out/app /usr/local/bin/app
USER app
EXPOSE 8080
CMD ["app"]
