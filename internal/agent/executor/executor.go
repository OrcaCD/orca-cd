package executor

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"time"

	pb "github.com/OrcaCD/orca-cd/api/proto"
	"github.com/rs/zerolog/log"
)

// Executor handles task execution on the agent
type Executor struct {
	runningTasks sync.Map // map[taskID]*runningTask
	mu           sync.RWMutex
}

type runningTask struct {
	ID        string
	Cancel    context.CancelFunc
	StartedAt time.Time
}

// NewExecutor creates a new task executor
func NewExecutor() *Executor {
	return &Executor{}
}

// RunningTaskCount returns the number of currently running tasks
func (e *Executor) RunningTaskCount() int {
	count := 0
	e.runningTasks.Range(func(key, value any) bool {
		count++
		return true
	})
	return count
}

// Execute runs a task and returns the result
func (e *Executor) Execute(task *pb.TaskAssignment) *pb.TaskResult {
	startedAt := time.Now()
	
	ctx, cancel := context.WithCancel(context.Background())
	if task.TimeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(task.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	// Track running task
	rt := &runningTask{
		ID:        task.TaskId,
		Cancel:    cancel,
		StartedAt: startedAt,
	}
	e.runningTasks.Store(task.TaskId, rt)
	defer e.runningTasks.Delete(task.TaskId)

	log.Info().
		Str("task_id", task.TaskId).
		Str("type", task.TaskType).
		Msg("Executing task")

	result := &pb.TaskResult{
		TaskId:      task.TaskId,
		StartedAt:   startedAt.Unix(),
		CompletedAt: 0,
	}

	var output []byte
	var err error

	switch task.TaskType {
	case "shell":
		output, err = e.executeShell(ctx, task.Payload)
	case "deploy":
		output, err = e.executeDeploy(ctx, task.Payload)
	case "build":
		output, err = e.executeBuild(ctx, task.Payload)
	default:
		result.Status = pb.TaskStatus_TASK_STATUS_FAILED
		result.ErrorMessage = "unknown task type: " + task.TaskType
		result.CompletedAt = time.Now().Unix()
		return result
	}

	result.CompletedAt = time.Now().Unix()

	if ctx.Err() == context.DeadlineExceeded {
		result.Status = pb.TaskStatus_TASK_STATUS_TIMEOUT
		result.ErrorMessage = "task timed out"
	} else if ctx.Err() == context.Canceled {
		result.Status = pb.TaskStatus_TASK_STATUS_CANCELLED
		result.ErrorMessage = "task was cancelled"
	} else if err != nil {
		result.Status = pb.TaskStatus_TASK_STATUS_FAILED
		result.ErrorMessage = err.Error()
	} else {
		result.Status = pb.TaskStatus_TASK_STATUS_SUCCESS
	}

	result.Output = output

	log.Info().
		Str("task_id", task.TaskId).
		Int32("status", int32(result.Status)).
		Dur("duration", time.Duration(result.CompletedAt-result.StartedAt)*time.Second).
		Msg("Task completed")

	return result
}

// Cancel cancels a running task
func (e *Executor) Cancel(taskID string) {
	if val, ok := e.runningTasks.Load(taskID); ok {
		rt := val.(*runningTask)
		rt.Cancel()
		log.Info().Str("task_id", taskID).Msg("Task cancelled")
	}
}

// ShellPayload represents the payload for shell tasks
type ShellPayload struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Dir     string   `json:"dir"`
}

func (e *Executor) executeShell(ctx context.Context, payload []byte) ([]byte, error) {
	var p ShellPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, p.Command, p.Args...)
	if p.Dir != "" {
		cmd.Dir = p.Dir
	}

	output, err := cmd.CombinedOutput()
	return output, err
}

// DeployPayload represents the payload for deploy tasks
type DeployPayload struct {
	Image       string            `json:"image"`
	Tag         string            `json:"tag"`
	Environment string            `json:"environment"`
	Config      map[string]string `json:"config"`
}

func (e *Executor) executeDeploy(ctx context.Context, payload []byte) ([]byte, error) {
	var p DeployPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}

	// TODO: Implement actual deployment logic
	log.Info().
		Str("image", p.Image).
		Str("tag", p.Tag).
		Str("env", p.Environment).
		Msg("Would deploy image")

	result := map[string]string{
		"status":  "deployed",
		"image":   p.Image,
		"tag":     p.Tag,
		"env":     p.Environment,
	}

	return json.Marshal(result)
}

// BuildPayload represents the payload for build tasks
type BuildPayload struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	Dockerfile string `json:"dockerfile"`
	Tag        string `json:"tag"`
}

func (e *Executor) executeBuild(ctx context.Context, payload []byte) ([]byte, error) {
	var p BuildPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}

	// TODO: Implement actual build logic
	log.Info().
		Str("repo", p.Repository).
		Str("branch", p.Branch).
		Str("tag", p.Tag).
		Msg("Would build image")

	result := map[string]string{
		"status": "built",
		"repo":   p.Repository,
		"branch": p.Branch,
		"tag":    p.Tag,
	}

	return json.Marshal(result)
}
