# Task Manager

This is a simple gRPC-based task manager service that allows clients to submit tasks, check their status, and stream status updates in real-time.

## Project Overview

The project consists of three main components:
- **`proto/`**: Contains the protobuf definition for the `TaskManager` service, including all RPC methods and message types.
- **`server/`**: The gRPC server implementation that handles task submissions and status updates.
- **`client/`**: A client application that demonstrates how to interact with the `TaskManager` service.

## gRPC Service API

The `TaskManager` service is defined in `proto/taskmanager.proto` and exposes the following RPC methods:

- **`SubmitTask(TaskRequest) returns (TaskResponse)`**: Submits a new task to the manager.
- **`CheckTaskStatus(StatusRequest) returns (StatusResponse)`**: Retrieves the current status of a specific task.
- **`StreamTaskStatus(StatusRequest) returns (stream StatusResponse)`**: Streams status updates for a task in real-time.

## Setup and Installation

To run this project, you need to have Go and Docker installed on your system.

1. **Clone the repository:**
   ```sh
   git clone <repository-url>
   cd task-manager
   ```

2. **Build the Docker containers:**
   ```sh
   docker-compose build
   ```

## Usage

To run the server and client, use the following command:

```sh
docker-compose up
```

The client will automatically connect to the server, submit a new task, and stream its status updates. You will see logs from both the server and client in your terminal.

### Observability

This project is instrumented with OpenTelemetry for tracing and metrics.

- **Jaeger:** To view traces, open your browser and navigate to `http://localhost:16686`.
- **Prometheus:** To view metrics, open your browser and navigate to `http://localhost:9090`.

### Running the Client and Server Manually

You can also run the server and client manually without Docker.

**Server:**
```sh
go run server/server.go
```

**Client:**
```sh
go run client/client.go
```