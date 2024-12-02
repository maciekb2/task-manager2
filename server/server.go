package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	pb "asynchronous-task-manager/proto" // Import wygenerowanego kodu z Protobuf (lokalnie w projekcie)

	"google.golang.org/grpc"
)

type server struct {
	pb.UnimplementedTaskManagerServer
	tasks map[string]string
	mu    sync.Mutex
}

func (s *server) SubmitTask(ctx context.Context, req *pb.TaskRequest) (*pb.TaskResponse, error) {
	taskID := fmt.Sprintf("%d", rand.Int())
	s.mu.Lock()
	s.tasks[taskID] = "PENDING"
	s.mu.Unlock()

	// Przetwarzanie zadania asynchronicznie
	go s.processTask(taskID, req.TaskDescription)

	return &pb.TaskResponse{TaskId: taskID}, nil
}

func (s *server) CheckTaskStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	s.mu.Lock()
	status, exists := s.tasks[req.TaskId]
	s.mu.Unlock()

	if !exists {
		return &pb.StatusResponse{Status: "UNKNOWN TASK"}, nil
	}
	return &pb.StatusResponse{Status: status}, nil
}

func (s *server) processTask(taskID string, taskDescription string) {
	// Symulacja długiego przetwarzania (np. 10 sekund)
	log.Printf("Processing task: %s\n", taskDescription)
	time.Sleep(10 * time.Second)

	// Aktualizacja statusu zadania po przetworzeniu
	s.mu.Lock()
	s.tasks[taskID] = "COMPLETED"
	s.mu.Unlock()
	log.Printf("Task %s completed\n", taskID)
}

func main() {
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	taskManagerServer := &server{
		tasks: make(map[string]string),
	}
	pb.RegisterTaskManagerServer(grpcServer, taskManagerServer)

	log.Println("Server is running on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
