package main

import (
	"context"
	"log"
	"time"

	pb "github.com/maciekb2/task-manager/proto"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"google.golang.org/grpc"
)

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
			semconv.ServiceName("taskmanager-client"),
		)),
	)
	return tp, nil
}

// main is the entry point for the client application.
// It connects to the gRPC server, submits a task, and streams its status.
func main() {
	// Initialize OpenTelemetry
	tp, err := tracerProvider("http://jaeger:14268/api/traces")
	if err != nil {
		log.Fatal(err)
	}
	otel.SetTracerProvider(tp)

	// Connect to the server.
	conn, err := grpc.Dial("taskmanager-service:50051",
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTaskManagerClient(conn)

	// Submit a new task with a priority.
	taskCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	taskDescription := "Sample Task"
	priority := "HIGH" // Priority can be changed dynamically.

	log.Printf("Submitting a new task: %s with priority: %s", taskDescription, priority)

	res, err := client.SubmitTask(taskCtx, &pb.TaskRequest{
		TaskDescription: taskDescription,
		Priority:        priority,
	})
	if err != nil {
		log.Fatalf("could not submit task: %v", err)
	}
	log.Printf("Task submitted with ID: %s", res.TaskId)

	// Stream the task status.
	statusCtx, cancelStatus := context.WithCancel(context.Background())
	defer cancelStatus()

	stream, err := client.StreamTaskStatus(statusCtx, &pb.StatusRequest{TaskId: res.TaskId})
	if err != nil {
		log.Fatalf("could not get status stream: %v", err)
	}

	log.Println("Waiting for status updates...")
	for {
		status, err := stream.Recv()
		if err != nil {
			log.Printf("Stream finished: %v", err)
			break
		}
		log.Printf("Task status [%s]: %s", res.TaskId, status.Status)
		if status.Status == "COMPLETED" || status.Status == "FAILED" {
			break
		}
	}
}
