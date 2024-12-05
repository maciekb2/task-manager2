package main

import (
	"context"
	"log"
	"time"

	pb "github.com/maciekb2/task-manager/proto"

	"google.golang.org/grpc"
)

func main() {
	// Połączenie z serwerem
	conn, err := grpc.Dial("taskmanager-service:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTaskManagerClient(conn)

	// Wysyłanie zadania z priorytetem
	taskCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	taskDescription := "Sample Task"
	priority := "HIGH" // Priorytet można zmieniać dynamicznie

	log.Printf("Wysyłanie nowego zadania: %s z priorytetem: %s", taskDescription, priority)

	res, err := client.SubmitTask(taskCtx, &pb.TaskRequest{
		TaskDescription: taskDescription,
		Priority:        priority,
	})
	if err != nil {
		log.Fatalf("could not submit task: %v", err)
	}
	log.Printf("Zadanie zostało wysłane z ID: %s", res.TaskId)

	// Strumieniowe monitorowanie statusu zadania
	statusCtx, cancelStatus := context.WithCancel(context.Background())
	defer cancelStatus()

	stream, err := client.StreamTaskStatus(statusCtx, &pb.StatusRequest{TaskId: res.TaskId})
	if err != nil {
		log.Fatalf("could not get status stream: %v", err)
	}

	log.Println("Oczekiwanie na statusy...")
	for {
		status, err := stream.Recv()
		if err != nil {
			log.Printf("Stream zakończony: %v", err)
			break
		}
		log.Printf("Status zadania [%s]: %s", res.TaskId, status.Status)
		if status.Status == "COMPLETED" || status.Status == "FAILED" {
			break
		}
	}
}
