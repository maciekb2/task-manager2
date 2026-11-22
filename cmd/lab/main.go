package main

import (
	"github.com/maciekb2/task-manager2/internal/lab"
	"log"
)

func main() {
	if err := lab.Run(); err != nil {
		log.Fatal(err)
	}
}
