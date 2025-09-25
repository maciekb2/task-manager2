package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"time"

	pb "github.com/maciekb2/task-manager/proto"
	"google.golang.org/grpc"
)

var (
	grpcAddr = "taskmanager-service:50051"
	client   pb.TaskManagerClient
)

func main() {
	// Set up a connection to the server.
	conn, err := grpc.Dial(grpcAddr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer conn.Close()
	client = pb.NewTaskManagerClient(conn)

	http.HandleFunc("/", dashboardHandler)
	http.HandleFunc("/submit", submitHandler)

	log.Println("UI server listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stats, err := client.GetStatistics(ctx, &pb.StatisticsRequest{})
	if err != nil {
		http.Error(w, "Error fetching statistics", http.StatusInternalServerError)
		log.Printf("could not get statistics: %v", err)
		return
	}

	tmpl, err := template.ParseFiles("templates/dashboard.html")
	if err != nil {
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		log.Printf("could not parse template: %v", err)
		return
	}

	err = tmpl.Execute(w, stats)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		log.Printf("could not execute template: %v", err)
	}
}

func submitHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	description := r.FormValue("description")
	priority := r.FormValue("priority")

	if description == "" {
		http.Error(w, "Description is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.SubmitTask(ctx, &pb.TaskRequest{
		TaskDescription: description,
		Priority:        priority,
	})
	if err != nil {
		http.Error(w, "Error submitting task", http.StatusInternalServerError)
		log.Printf("could not submit task: %v", err)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}