package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	pb "github.com/maciekb2/task-manager/proto" // Import wygenerowanego kodu z Protobuf

	"google.golang.org/grpc"
)

type task struct {
	id          string
	description string
	priority    string
	status      string
}

type server struct {
	pb.UnimplementedTaskManagerServer
	tasks       map[string]*task
	mu          sync.Mutex
	subscribers map[string]chan string
}

func newServer() *server {
	return &server{
		tasks:       make(map[string]*task),
		subscribers: make(map[string]chan string),
	}
}

// SubmitTask: Dodaje zadanie z priorytetem
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

	// Asynchroniczne przetwarzanie zadania
	go s.processTask(taskID)

	return &pb.TaskResponse{TaskId: taskID}, nil
}

// CheckTaskStatus: Zwraca aktualny status zadania
func (s *server) CheckTaskStatus(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	s.mu.Lock()
	task, exists := s.tasks[req.TaskId]
	s.mu.Unlock()

	if !exists {
		return &pb.StatusResponse{Status: "UNKNOWN TASK"}, nil
	}

	return &pb.StatusResponse{Status: task.status}, nil
}

// StreamTaskStatus: Wysyła status zadania w czasie rzeczywistym
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

func (s *server) GetStatistics(ctx context.Context, req *pb.StatisticsRequest) (*pb.StatisticsResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stats := &pb.StatisticsResponse{}
	for _, task := range s.tasks {
		switch task.status {
		case "QUEUED":
			stats.Queued++
		case "IN_PROGRESS":
			stats.InProgress++
		case "COMPLETED":
			stats.Completed++
		case "FAILED":
			stats.Failed++
		}
	}
	return stats, nil
}

// processTask: Symuluje przetwarzanie zadania
func (s *server) processTask(taskID string) {
	s.mu.Lock()
	task := s.tasks[taskID]
	s.mu.Unlock()

	// Aktualizacja statusu na IN_PROGRESS
	s.updateTaskStatus(taskID, "IN_PROGRESS")
	time.Sleep(5 * time.Second) // Symulacja przetwarzania

	// Symulacja sukcesu lub porażki
	if rand.Float32() < 0.8 {
		s.updateTaskStatus(taskID, "COMPLETED")
	} else {
		s.updateTaskStatus(taskID, "FAILED")
	}
}

// updateTaskStatus: Aktualizuje status zadania i powiadamia subskrybentów
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

func main() {
	// Inicjalizacja serwera
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterTaskManagerServer(grpcServer, newServer())

	log.Println("Serwer gRPC działa na porcie :50051")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
