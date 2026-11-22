FROM golang:1.26.0-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG VERSION=dev
ARG DEMO_FAULT=
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/maciekb2/task-manager2/internal/lab.demoFault=${DEMO_FAULT}" -o /lab ./cmd/lab

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 lab
COPY --from=build /lab /usr/local/bin/lab
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/lab"]
