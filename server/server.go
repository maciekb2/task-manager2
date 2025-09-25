package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	pb "github.com/maciekb2/task-manager/proto"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"google.golang.org/grpc"
)

// task represents a single task with its properties.
type task struct {
	id          string
	description string
	priority    string
	status      string
}

// server is the gRPC server implementation for the TaskManager service.
type server struct {
	pb.UnimplementedTaskManagerServer
	tasks       map[string]*task
	mu          sync.Mutex
	subscribers map[string]chan string
}

// newServer creates a new server instance.
func newServer() *server {
	return &server{
		tasks:       make(map[string]*task),
		subscribers: make(map[string]chan string),
	}
}

// SubmitTask adds a new task with a given priority.
// It returns a TaskResponse with the new task's ID or an error.
func (s *server) SubmitTask(ctx context.Context, req *pb.TaskRequest) (*pb.TaskResponse, error) {
	taskID := fmt.Sprintf("%d", rand.Int())
	task := &task{
		id:          taskID,
		description: req.TaskDescription,
		priority:    req.Priority,
		status:      "QUEUED",
	}

	s.mu.Lock()
	s.tasks[taskID] = task
	if _, exists := s.subscribers[taskID]; !exists {
		s.subscribers[taskID] = make(chan string, 10)
	}
	s.mu.Unlock()

	// Asynchronously process the task.
	go s.processTask(taskID)

	return &pb.TaskResponse{TaskId: taskID}, nil
}

// CheckTaskStatus returns the current status of a task.
// It returns a StatusResponse with the task's status or an error if the task is not found.
func (s *server) CheckTaskStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	s.mu.Lock()
	task, exists := s.tasks[req.TaskId]
	s.mu.Unlock()

	if !exists {
		return &pb.StatusResponse{Status: "UNKNOWN TASK"}, nil
	}

	return &pb.StatusResponse{Status: task.status}, nil
}

// StreamTaskStatus sends the status of a task in real-time.
// It streams StatusResponse messages to the client.
func (s *server) StreamTaskStatus(req *pb.StatusRequest, stream pb.TaskManager_StreamTaskStatusServer) error {
	s.mu.Lock()
	ch, exists := s.subscribers[req.TaskId]
	s.mu.Unlock()

	if !exists {
		return fmt.Errorf("task not found")
	}

	for status := range ch {
		if err := stream.Send(&pb.StatusResponse{Status: status}); err != nil {
			return err
		}
		if status == "COMPLETED" || status == "FAILED" {
			break
		}
	}
	return nil
}

// processTask simulates the processing of a task.
func (s *server) processTask(taskID string) {
	s.mu.Lock()
	task := s.tasks[taskID]
	s.mu.Unlock()

	// Update status to IN_PROGRESS.
	s.updateTaskStatus(taskID, "IN_PROGRESS")
	time.Sleep(5 * time.Second) // Simulate processing time.

	// Simulate success or failure.
	if rand.Float32() < 0.8 {
		s.updateTaskStatus(taskID, "COMPLETED")
	} else {
		s.updateTaskStatus(taskID, "FAILED")
	}
}

// updateTaskStatus updates the status of a task and notifies subscribers.
func (s *server) updateTaskStatus(taskID, status string) {
	s.mu.Lock()
	if task, exists := s.tasks[taskID]; exists {
		task.status = status
		if ch, ok := s.subscribers[taskID]; ok {
			ch <- status
		}
	}
	s.mu.Unlock()
}

// tracerProvider returns an OpenTelemetry TracerProvider configured to use
// the Jaeger exporter.
func tracerProvider(url string) (*tracesdk.TracerProvider, error) {
	// Create the Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, err
	}
	tp := tracesdk.NewTracerProvider(
		// Always be sure to batch in production.
		tracesdk.WithBatcher(exp),
		// Record information about this application in a Resource.
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("taskmanager-server"),
		)),
	)
	return tp, nil
}

// main is the entry point for the server application.
// It initializes the gRPC server and starts listening for incoming connections.
func main() {
	// Initialize the server.
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// Initialize OpenTelemetry
	tp, err := tracerProvider("http://jaeger:14268/api/traces")
	if err != nil {
		log.Fatal(err)
	}
	otel.SetTracerProvider(tp)

	// Create a new Prometheus exporter and register it as a provider.
	exporter, err := prometheus.New()
	if err != nil {
		log.Fatal(err)
	}

	// Create a gRPC server with the OpenTelemetry interceptor.
	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(otelgrpc.UnaryServerInterceptor()),
		grpc.StreamInterceptor(otelgrpc.StreamServerInterceptor()),
	)
	pb.RegisterTaskManagerServer(grpcServer, newServer())

	// Start a separate HTTP server for metrics and health checks.
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		log.Fatal(http.ListenAndServe(":8080", nil))
	}()

	log.Println("gRPC server is running on port :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
