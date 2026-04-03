package tui

import (
	"context"
	"fmt"
	"strings"

	"ecs-connect/internal/cloud"
	"ecs-connect/internal/recents"
)

// ReconnectToRecents loads a saved ECS exec target for the client's profile.
// historyIndex is 0 = most recent (--reconnect), 1 = previous (--reconnect=prev),
// 2 = third (--reconnect=old). The task must be RUNNING and the container must
// still exist on the task.
func ReconnectToRecents(ctx context.Context, client *cloud.Client, historyIndex int) (*Outcome, error) {
	if client == nil {
		return nil, fmt.Errorf("AWS client is nil")
	}
	if historyIndex < 0 || historyIndex > 2 {
		return nil, fmt.Errorf("invalid reconnect slot %d (use 0–2)", historyIndex)
	}
	prof := client.Profile
	all, ok := recents.LoadAll(prof)
	if !ok || len(all) == 0 {
		return nil, fmt.Errorf("no saved ECS targets for profile %q — connect interactively once, or see ~/.ecs-connect/recents.json", prof)
	}
	if historyIndex >= len(all) {
		slot := reconnectSlotLabel(historyIndex)
		return nil, fmt.Errorf("profile %q has only %d saved target(s); --reconnect=%s needs at least %d (newest-first: recent, prev, old)",
			prof, len(all), slot, historyIndex+1)
	}
	rt := all[historyIndex]
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

func reconnectSlotLabel(i int) string {
	switch i {
	case 0:
		return "recent"
	case 1:
		return "prev"
	case 2:
		return "old"
	default:
		return fmt.Sprintf("index%d", i)
	}
}
