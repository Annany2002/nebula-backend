package models

import "github.com/Annany2002/nebula-backend/internal/domain"

// CreateTriggerRequest defines a persistent trigger on a user table.
type CreateTriggerRequest = domain.TriggerDefinition

// CreateTriggerResponse returns the same trigger metadata as the objects catalog.
type CreateTriggerResponse struct {
	Message string             `json:"message"`
	DBName  string             `json:"db_name"`
	Trigger domain.TriggerInfo `json:"trigger"`
}

// DropTriggerResponse acknowledges removal without changing existing records.
type DropTriggerResponse struct {
	Message     string `json:"message"`
	DBName      string `json:"db_name"`
	TriggerName string `json:"trigger_name"`
}
