package cloud

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Client wraps the AWS SDK clients needed for ECS connect operations.
type Client struct {
	ecs     *ecs.Client
	sts     *sts.Client
	Region  string
	Profile string
}

// New creates an AWS Client configured for the given profile and region.
func New(profile, region string) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithSharedConfigProfile(profile),
		awsconfig.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &Client{
		ecs:     ecs.NewFromConfig(cfg),
		sts:     sts.NewFromConfig(cfg),
		Region:  region,
		Profile: profile,
	}, nil
}

// CheckAuth validates AWS credentials and returns the caller ARN.
func (c *Client) CheckAuth(ctx context.Context) (string, error) {
	out, err := c.sts.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Arn), nil
}

// ListClusters returns all ECS cluster names (paginated).
func (c *Client) ListClusters(ctx context.Context) ([]string, error) {
	var names []string
	p := ecs.NewListClustersPaginator(c.ecs, &ecs.ListClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, arn := range page.ClusterArns {
			names = append(names, arnName(arn))
		}
	}
	sort.Strings(names)
	return names, nil
}

// ListServices returns all service names in the given cluster (paginated).
func (c *Client) ListServices(ctx context.Context, cluster string) ([]string, error) {
	var names []string
	p := ecs.NewListServicesPaginator(c.ecs, &ecs.ListServicesInput{
		Cluster: aws.String(cluster),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, arn := range page.ServiceArns {
			names = append(names, arnName(arn))
		}
	}
	sort.Strings(names)
	return names, nil
}

// ServiceInfo holds the relevant fields from an ECS DescribeServices response.
type ServiceInfo struct {
	Status       string
	DesiredCount int32
	RunningCount int32
	PendingCount int32
	TaskDef      string
}

// DescribeService returns status and count details for a single service.
func (c *Client) DescribeService(ctx context.Context, cluster, service string) (*ServiceInfo, error) {
	out, err := c.ecs.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(cluster),
		Services: []string{service},
	})
	if err != nil {
		return nil, err
	}
	if len(out.Services) == 0 {
		return nil, fmt.Errorf("service %q not found", service)
	}
	s := out.Services[0]
	return &ServiceInfo{
		Status:       aws.ToString(s.Status),
		DesiredCount: s.DesiredCount,
		RunningCount: s.RunningCount,
		PendingCount: s.PendingCount,
		TaskDef:      arnName(aws.ToString(s.TaskDefinition)),
	}, nil
}

// TaskInfo holds details about a running ECS task.
type TaskInfo struct {
	ARN        string
	ShortID    string
	CreatedAt  time.Time
	Status     string
	Containers []string
}

// ListRunningTasks returns running tasks for a service, sorted newest-first.
func (c *Client) ListRunningTasks(ctx context.Context, cluster, service string) ([]TaskInfo, error) {
	var arns []string
	p := ecs.NewListTasksPaginator(c.ecs, &ecs.ListTasksInput{
		Cluster:       aws.String(cluster),
		ServiceName:   aws.String(service),
		DesiredStatus: ecstypes.DesiredStatusRunning,
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		arns = append(arns, page.TaskArns...)
	}
	if len(arns) == 0 {
		return nil, nil
	}

	// DescribeTasks accepts max 100 ARNs; batch if needed
	var tasks []TaskInfo
	for i := 0; i < len(arns); i += 100 {
		end := i + 100
		if end > len(arns) {
			end = len(arns)
		}
		desc, err := c.ecs.DescribeTasks(ctx, &ecs.DescribeTasksInput{
			Cluster: aws.String(cluster),
			Tasks:   arns[i:end],
		})
		if err != nil {
			return nil, err
		}
		for _, t := range desc.Tasks {
			var containers []string
			for _, ct := range t.Containers {
				containers = append(containers, aws.ToString(ct.Name))
			}
			ti := TaskInfo{
				ARN:        aws.ToString(t.TaskArn),
				ShortID:    arnName(aws.ToString(t.TaskArn)),
				Status:     aws.ToString(t.LastStatus),
				Containers: containers,
			}
			if t.CreatedAt != nil {
				ti.CreatedAt = *t.CreatedAt
			}
			tasks = append(tasks, ti)
		}
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})
	return tasks, nil
}

// ExecSession holds the session-manager-plugin connection details.
type ExecSession struct {
	SessionID  string
	StreamURL  string
	TokenValue string
}

// ExecuteCommand starts an ECS Exec session and returns the connection details.
func (c *Client) ExecuteCommand(cluster, task, container, command string) (*ExecSession, error) {
	out, err := c.ecs.ExecuteCommand(context.Background(), &ecs.ExecuteCommandInput{
		Cluster:     aws.String(cluster),
		Task:        aws.String(task),
		Container:   aws.String(container),
		Command:     aws.String(command),
		Interactive: true,
	})
	if err != nil {
		return nil, err
	}
	if out.Session == nil {
		return nil, fmt.Errorf("API returned no session — is ECS Exec enabled on this service?")
	}
	return &ExecSession{
		SessionID:  aws.ToString(out.Session.SessionId),
		StreamURL:  aws.ToString(out.Session.StreamUrl),
		TokenValue: aws.ToString(out.Session.TokenValue),
	}, nil
}

func arnName(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}
