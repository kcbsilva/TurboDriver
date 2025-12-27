FROM golang:1.22 AS builder
WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o turbodriver ./cmd/server

FROM gcr.io/distroless/base-debian12
WORKDIR /srv/app
COPY --from=builder /app/turbodriver .
ENV HTTP_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["./turbodriver"]
