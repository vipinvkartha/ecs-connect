package tui

import (
	"context"
	"fmt"
	"strings"

	"ecs-connect/internal/cloud"
	"ecs-connect/internal/recents"
)

// ReconnectToRecents loads the last successful ECS exec target for the client's
// AWS profile, verifies the task is RUNNING and the container still exists,
// and returns an outcome without opening the interactive wizard.
func ReconnectToRecents(ctx context.Context, client *cloud.Client) (*Outcome, error) {
	if client == nil {
		return nil, fmt.Errorf("AWS client is nil")
	}
	prof := client.Profile
	rt, ok := recents.Load(prof)
	if !ok {
		return nil, fmt.Errorf("no saved ECS target for profile %q — connect interactively once, or see ~/.ecs-connect/recents.json", prof)
	}
	info, err := client.DescribeTask(ctx, rt.Cluster, rt.TaskARN)
	if err != nil {
		return nil, fmt.Errorf("describe task: %w", err)
	}
	if !strings.EqualFold(info.Status, "RUNNING") {
		return nil, fmt.Errorf("task %s is %s (expected RUNNING)", rt.TaskShortID, info.Status)
	}
	found := false
	for _, name := range info.Containers {
		if name == rt.Container {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("container %q not in task (have %v)", rt.Container, info.Containers)
	}
	return &Outcome{
		Mode: ModeECS,
		ECS: &Result{
			Profile:     prof,
			Environment: rt.Environment,
			Cluster:     rt.Cluster,
			AppGroup:    rt.AppGroup,
			Service:     rt.Service,
			Slug:        rt.Slug,
			TaskARN:     rt.TaskARN,
			TaskShortID: info.ShortID,
			Container:   rt.Container,
		},
	}, nil
}
