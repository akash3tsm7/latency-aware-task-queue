package models

import "github.com/google/uuid"

type Worker struct {
	ID   string
	Type string // cpu | gpu
}

func NewWorker(workerType string) Worker {
	return Worker{
		ID:   uuid.New().String(),
		Type: workerType,
	}
}
