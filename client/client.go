package main

import (
	"context"
	"log"
	"time"

	pb "github.com/maciekb2/task-manager/proto" // Import wygenerowanego kodu z Protobuf (lokalnie w projekcie)

	"google.golang.org/grpc"
)

func main() {
	conn, err := grpc.Dial("taskmanager-service:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTaskManagerClient(conn)

	// Wysyłamy nowe zadanie
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	res, err := client.SubmitTask(ctx, &pb.TaskRequest{TaskDescription: "Sample Task"})
	if err != nil {
		log.Fatalf("could not submit task: %v", err)
	}
	log.Printf("Task submitted with ID: %s", res.TaskId)

	// Sprawdzamy status zadania po 5 sekundach
	time.Sleep(5 * time.Second)

	statusRes, err := client.CheckTaskStatus(ctx, &pb.StatusRequest{TaskId: res.TaskId})
	if err != nil {
		log.Fatalf("could not check task status: %v", err)
	}
	log.Printf("Task status: %s", statusRes.Status)
}
