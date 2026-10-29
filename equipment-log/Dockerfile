FROM golang:1.22-alpine AS builder
WORKDIR /build
COPY main.go go.mod .
RUN CGO_ENABLED=0 GOOS=linux go build -o equipment-log .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /opt/equipment-log
COPY --from=builder /build/equipment-log .
COPY app ./app
ENV PORT=8787
EXPOSE 8787
CMD ["./equipment-log"]
