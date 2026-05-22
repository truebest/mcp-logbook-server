package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
)

// LogLevel represents the severity level of a log entry
type LogLevel string

const (
	LogLevelDebug LogLevel = "DEBUG"
	LogLevelInfo  LogLevel = "INFO"
	LogLevelWarn  LogLevel = "WARN"
	LogLevelError LogLevel = "ERROR"
	LogLevelFatal LogLevel = "FATAL"
)

// Platform represents the platform/SDK that generated the log
type Platform string

const (
	PlatformGo          Platform = "go"
	PlatformSwift       Platform = "swift"
	PlatformExpress     Platform = "express"
	PlatformReact       Platform = "react"
	PlatformReactNative Platform = "react-native"
	PlatformKotlin      Platform = "kotlin"
)

// DeviceInfo contains platform-specific device information
type DeviceInfo struct {
	Platform   string `json:"platform" validate:"required"`
	Version    string `json:"version"`
	Model      string `json:"model"`
	AppVersion string `json:"app_version"`
}

// SourceLocation contains information about where the log was generated
type SourceLocation struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function"`
}

// LogEntry represents a single log entry in the system
type LogEntry struct {
	ID             string                 `json:"id" validate:"required,uuid4"`
	Timestamp      time.Time              `json:"timestamp" validate:"required"`
	Level          LogLevel               `json:"level" validate:"required,oneof=DEBUG INFO WARN ERROR FATAL"`
	Message        string                 `json:"message" validate:"required,max=10000,log_message"`
	ServiceName    string                 `json:"service_name" validate:"required,max=100,service_name"`
	AgentID        string                 `json:"agent_id" validate:"required,max=100,agent_id"`
	Platform       Platform               `json:"platform" validate:"required,oneof=go swift express react react-native kotlin"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	DeviceInfo     *DeviceInfo            `json:"device_info,omitempty"`
	StackTrace     string                 `json:"stack_trace,omitempty"`
	SourceLocation *SourceLocation        `json:"source_location,omitempty"`
}

// Validate validates the log entry using struct tags
func (le *LogEntry) Validate() error {
	validate := validator.New()

	// Register custom validators (same as in validation package)
	validate.RegisterValidation("service_name", func(fl validator.FieldLevel) bool {
		serviceName := fl.Field().String()
		matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, serviceName)
		return matched
	})

	validate.RegisterValidation("agent_id", func(fl validator.FieldLevel) bool {
		agentID := fl.Field().String()
		matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, agentID)
		return matched
	})

	validate.RegisterValidation("log_message", func(fl validator.FieldLevel) bool {
		message := fl.Field().String()
		return len(strings.TrimSpace(message)) > 0
	})

	return validate.Struct(le)
}

// ToJSON converts the log entry to JSON bytes
func (le *LogEntry) ToJSON() ([]byte, error) {
	return json.Marshal(le)
}

// FromJSON creates a log entry from JSON bytes
func FromJSON(data []byte) (*LogEntry, error) {
	var le LogEntry
	if err := json.Unmarshal(data, &le); err != nil {
		return nil, err
	}
	return &le, nil
}

// LogFilter represents filtering criteria for log queries
type LogFilter struct {
	ServiceName     string    `json:"service_name,omitempty"`
	AgentID         string    `json:"agent_id,omitempty"`
	Level           LogLevel  `json:"level,omitempty"`
	StartTime       time.Time `json:"start_time,omitempty"`
	EndTime         time.Time `json:"end_time,omitempty"`
	MessageContains string    `json:"message_contains,omitempty"`
	Platform        Platform  `json:"platform,omitempty"`
	Limit           int       `json:"limit,omitempty"`
	Offset          int       `json:"offset,omitempty"`
}

// LogResult represents the result of a log query
type LogResult struct {
	Logs       []LogEntry `json:"logs"`
	TotalCount int        `json:"total_count"`
	HasMore    bool       `json:"has_more"`
}

// HealthStatus represents the health status of a service
type HealthStatus struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Details   map[string]string `json:"details,omitempty"`
}

// ServiceInfo represents information about a service
type ServiceInfo struct {
	ServiceName string    `json:"service_name"`
	AgentID     string    `json:"agent_id"`
	Platform    Platform  `json:"platform"`
	LastSeen    time.Time `json:"last_seen"`
	LogCount    int       `json:"log_count"`
}

// WorkEntryContentBlock represents one structured content block in a work-log entry.
type WorkEntryContentBlock struct {
	Type     string `json:"type" validate:"required,oneof=text console code"`
	Text     string `json:"text" validate:"required,max=20000"`
	Title    string `json:"title,omitempty" validate:"max=255"`
	Language string `json:"language,omitempty" validate:"max=64"`
	Command  string `json:"command,omitempty" validate:"max=1000"`
	ExitCode *int   `json:"exit_code,omitempty"`
}

// WorkEntry represents one agent work-log entry for a task.
type WorkEntry struct {
	ID                 string                  `json:"id" validate:"required,uuid4"`
	OrchestratorTaskID string                  `json:"orchestrator_task_id" validate:"required,task_id"`
	AgentTaskID        string                  `json:"agent_task_id" validate:"required,task_id"`
	Text               string                  `json:"text" validate:"max=20000"`
	Content            []WorkEntryContentBlock `json:"content,omitempty" validate:"omitempty,dive"`
	AgentID            string                  `json:"agent_id" validate:"required,max=100,agent_id"`
	GitBranch          string                  `json:"git_branch" validate:"required,max=255"`
	Timestamp          time.Time               `json:"timestamp" validate:"required"`
	Metadata           map[string]interface{}  `json:"metadata,omitempty"`
}

// Validate validates a work-log entry.
func (we *WorkEntry) Validate() error {
	validate := validator.New()

	validate.RegisterValidation("task_id", func(fl validator.FieldLevel) bool {
		taskID := fl.Field().String()
		matched, _ := regexp.MatchString(`^[A-Z][A-Z0-9]+-[0-9]+$`, taskID)
		return matched
	})

	validate.RegisterValidation("agent_id", func(fl validator.FieldLevel) bool {
		agentID := fl.Field().String()
		matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+$`, agentID)
		return matched
	})

	hasBody := strings.TrimSpace(we.Text) != ""
	for _, block := range we.Content {
		if strings.TrimSpace(block.Text) != "" {
			hasBody = true
			break
		}
	}
	if !hasBody {
		return fmt.Errorf("work entry requires non-empty text or content")
	}

	return validate.Struct(we)
}

// WorkEntryFilter represents filtering and pagination for task work entries.
type WorkEntryFilter struct {
	OrchestratorTaskID string    `json:"orchestrator_task_id,omitempty"`
	AgentTaskID        string    `json:"agent_task_id,omitempty"`
	AgentID            string    `json:"agent_id,omitempty"`
	StartTime          time.Time `json:"start_time,omitempty"`
	EndTime            time.Time `json:"end_time,omitempty"`
	Limit              int       `json:"limit,omitempty"`
	Offset             int       `json:"offset,omitempty"`
}

// WorkEntryResult represents a paginated work-log query result.
type WorkEntryResult struct {
	Entries    []WorkEntry `json:"entries"`
	TotalCount int         `json:"total_count"`
	HasMore    bool        `json:"has_more"`
}

// TaskInfo summarizes one task's work-log activity.
type TaskInfo struct {
	OrchestratorTaskID string    `json:"orchestrator_task_id"`
	AgentTaskID        string    `json:"agent_task_id"`
	EntryCount         int       `json:"entry_count"`
	LastEntryAt        time.Time `json:"last_entry_at"`
	Agents             []string  `json:"agents"`
	GitBranches        []string  `json:"git_branches"`
}
