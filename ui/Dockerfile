# Build stage
FROM golang:1.21-alpine as builder
WORKDIR /app
COPY go.mod ./
RUN go mod tidy
COPY . .
RUN go build -o /app/ui-server .

# Final stage
FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/ui-server .
COPY templates ./templates
EXPOSE 8080
CMD ["./ui-server"]