FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /edms-load-tester main.go

FROM alpine:3.19
RUN apk --no-cache add ca-certificates

COPY --from=builder /edms-load-tester /usr/local/bin/edms-load-tester

EXPOSE 9100

ENTRYPOINT ["edms-load-tester"]
CMD ["-target=http://webserver:3000", "-concurrency=20", "-requests=5000"]
