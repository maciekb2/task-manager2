package main

import (
	"context"
	"log"
	"time"

	pb "github.com/maciekb2/task-manager/proto" // Import wygenerowanego kodu z Protobuf (lokalnie w projekcie)

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func main() {
	conn, err := grpc.Dial("taskmanager-service:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewTaskManagerClient(conn)

	for {
		// Wysyłamy nowe zadanie
		taskCtx, taskCancel := context.WithTimeout(context.Background(), 15*time.Second)
		taskDescription := "Sample Task"
		log.Printf("Wysyłanie nowego zadania: %s", taskDescription)

		res, err := client.SubmitTask(taskCtx, &pb.TaskRequest{TaskDescription: taskDescription})
		taskCancel()
		if err != nil {
			log.Fatalf("could not submit task: %v", err)
		}
		log.Printf("Zadanie zostało wysłane z ID: %s", res.TaskId)

		// Sprawdzamy status zadania po 5 sekundach
		time.Sleep(5 * time.Second)

		statusCtx, statusCancel := context.WithTimeout(context.Background(), 15*time.Second)
		log.Printf("Sprawdzanie statusu zadania o ID: %s", res.TaskId)

		statusRes, err := client.CheckTaskStatus(statusCtx, &pb.StatusRequest{TaskId: res.TaskId})
		statusCancel()
		if err != nil {
			if status.Code(err) == codes.NotFound {
				log.Printf("Zadanie o ID %s nie zostało znalezione: %v", res.TaskId, err)
			} else if status.Code(err) == codes.DeadlineExceeded {
				log.Printf("Czas na sprawdzenze statusu zadania o ID %s przekroczony. Spróbuj ponownie później.", res.TaskId)
			} else {
				log.Fatalf("could not check task status: %v", err)
			}
		} else {
			log.Printf("Status zadania o ID %s: %s", res.TaskId, statusRes.Status)
		}

		// Opcjonalna przerwa między iteracjami
		time.Sleep(2 * time.Second)
	}
}
