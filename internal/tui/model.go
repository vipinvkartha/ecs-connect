package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"ecs-connect/internal/cloud"
	"ecs-connect/internal/naming"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user aborts the wizard.
var ErrCancelled = errors.New("cancelled")

// Result holds every value the wizard collected.
type Result struct {
	Profile     string
	Environment string
	Cluster     string
	AppGroup    string
	Service     string
	Slug        string
	TaskARN     string
	TaskShortID string
	Container   string
}

// Options configures the TUI wizard.
type Options struct {
	Client         *cloud.Client // pre-built client (skips profile selection)
	Profiles       []string      // available AWS profiles for interactive selection
	DefaultProfile string        // pre-highlighted profile in the picker
	Region         string        // AWS region (needed when creating client from profile)
}

// Run launches the interactive TUI wizard and returns the collected result
// plus the AWS client used during discovery (reusable for exec).
func Run(opts Options) (*Result, *cloud.Client, error) {
	m := newModel(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, nil, err
	}
	fm := final.(model)
	if fm.cancelled {
		return nil, nil, ErrCancelled
	}
	if fm.err != nil {
		return nil, nil, fm.err
	}
	return fm.result, fm.client, nil
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

type step int

const (
	stepSelectProfile step = iota
	stepCheckAuth
	stepSelectEnv
	stepLoadClusters
	stepSelectCluster
	stepLoadServices
	stepSelectService
	stepConfirm
	stepLoadTasks
	stepSelectTask
	stepSelectContainer
	stepDone
)

// ---------------------------------------------------------------------------
// Messages (async command results)
// ---------------------------------------------------------------------------

type (
	authOKMsg  string
	authErrMsg struct{ err error }

	clientReadyMsg struct {
		client *cloud.Client
		arn    string
	}

	clustersMsg []string
	servicesMsg []string
	tasksMsg    []cloud.TaskInfo

	previewMsg struct {
		slug string
		info *cloud.ServiceInfo
		err  error
	}
	errMsg struct{ err error }
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type model struct {
	step   step
	client *cloud.Client

	// terminal dimensions
	width, height int

	// shared widgets
	spinner    spinner.Model
	loadingMsg string

	// profile selection
	profileItems  []string
	profileCursor int
	profile       string
	region        string

	// list-selection state
	envItems       []string
	clusterItems   []string
	serviceItems   []string // slugs
	taskItems      []cloud.TaskInfo
	containerItems []string

	envCursor       int
	clusterCursor   int
	serviceCursor   int
	taskCursor      int
	containerCursor int

	// production confirmation
	confirmInput textinput.Model

	// service preview
	previewCache   map[string]*cloud.ServiceInfo
	currentPreview *cloud.ServiceInfo
	previewLoading bool

	// collected values
	environment string
	cluster     string
	appGroup    string
	service     string
	slug        string
	taskARN     string
	taskShortID string
	container   string
	authARN     string

	// terminal state
	cancelled bool
	err       error
	result    *Result
}

func newModel(opts Options) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ti := textinput.New()
	ti.Placeholder = "type 'yes' to continue"
	ti.CharLimit = 3

	m := model{
		client:       opts.Client,
		spinner:      s,
		confirmInput: ti,
		envItems:     []string{"staging", "production"},
		previewCache: make(map[string]*cloud.ServiceInfo),
		profileItems: opts.Profiles,
		profile:      opts.DefaultProfile,
		region:       opts.Region,
	}

	if opts.Client != nil || len(opts.Profiles) == 0 {
		m.step = stepCheckAuth
		if opts.Client != nil {
			m.profile = opts.Client.Profile
		}
	} else {
		m.step = stepSelectProfile
		for i, p := range opts.Profiles {
			if p == opts.DefaultProfile {
				m.profileCursor = i
				break
			}
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
	if m.step == stepSelectProfile {
		return m.spinner.Tick
	}
	return tea.Batch(m.spinner.Tick, m.checkAuth())
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancelled = true
			return m, tea.Quit
		}
		if msg.String() == "esc" && m.step != stepConfirm {
			m.cancelled = true
			return m, tea.Quit
		}

		switch m.step {
		case stepSelectProfile:
			if listNav(msg, &m.profileCursor, len(m.profileItems)) {
				m.profile = m.profileItems[m.profileCursor]
				m.step = stepCheckAuth
				return m, m.initClient()
			}

		case stepSelectEnv:
			if listNav(msg, &m.envCursor, len(m.envItems)) {
				m.environment = m.envItems[m.envCursor]
				m.loadingMsg = "Loading clusters..."
				m.step = stepLoadClusters
				return m, m.loadClusters()
			}

		case stepSelectCluster:
			if listNav(msg, &m.clusterCursor, len(m.clusterItems)) {
				m.cluster = m.clusterItems[m.clusterCursor]
				m.appGroup = naming.AppGroup(m.cluster, m.environment)
				m.loadingMsg = "Loading services..."
				m.step = stepLoadServices
				return m, m.loadServices()
			}

		case stepSelectService:
			prev := m.serviceCursor
			switch msg.String() {
			case "up", "k":
				if m.serviceCursor > 0 {
					m.serviceCursor--
				}
			case "down", "j":
				if m.serviceCursor < len(m.serviceItems)-1 {
					m.serviceCursor++
				}
			case "enter":
				m.slug = m.serviceItems[m.serviceCursor]
				m.service = naming.SlugToServiceName(m.slug, m.appGroup, m.environment)
				if m.environment == "production" {
					m.step = stepConfirm
					m.confirmInput.Focus()
					return m, textinput.Blink
				}
				m.loadingMsg = "Loading tasks..."
				m.step = stepLoadTasks
				return m, m.loadTasks()
			}
			if m.serviceCursor != prev {
				slug := m.serviceItems[m.serviceCursor]
				if cached, ok := m.previewCache[slug]; ok {
					m.currentPreview = cached
					m.previewLoading = false
				} else {
					m.currentPreview = nil
					m.previewLoading = true
					return m, m.fetchPreview(slug)
				}
			}

		case stepConfirm:
			switch msg.String() {
			case "enter":
				if m.confirmInput.Value() == "yes" {
					m.loadingMsg = "Loading tasks..."
					m.step = stepLoadTasks
					return m, m.loadTasks()
				}
				m.cancelled = true
				return m, tea.Quit
			case "esc":
				m.cancelled = true
				return m, tea.Quit
			default:
				var cmd tea.Cmd
				m.confirmInput, cmd = m.confirmInput.Update(msg)
				return m, cmd
			}

		case stepSelectTask:
			if listNav(msg, &m.taskCursor, len(m.taskItems)) {
				t := m.taskItems[m.taskCursor]
				m.taskARN = t.ARN
				m.taskShortID = t.ShortID
				if len(t.Containers) == 1 {
					m.container = t.Containers[0]
					return m, m.done()
				}
				m.containerItems = t.Containers
				m.step = stepSelectContainer
			}

		case stepSelectContainer:
			if listNav(msg, &m.containerCursor, len(m.containerItems)) {
				m.container = m.containerItems[m.containerCursor]
				return m, m.done()
			}
		}

	// spinner animation
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	// --- async results ---

	case authOKMsg:
		m.authARN = string(msg)
		m.step = stepSelectEnv
		return m, nil

	case authErrMsg:
		m.err = fmt.Errorf(
			"not authenticated — run:\n\n  aws sso login --profile %s", m.profile)
		return m, tea.Quit

	case clientReadyMsg:
		m.client = msg.client
		m.authARN = msg.arn
		m.step = stepSelectEnv
		return m, nil

	case clustersMsg:
		items := filterClusters([]string(msg), m.environment)
		if len(items) == 0 {
			m.err = fmt.Errorf("no ECS clusters found ending with -%s", m.environment)
			return m, tea.Quit
		}
		m.clusterItems = items
		m.step = stepSelectCluster
		return m, nil

	case servicesMsg:
		slugs := extractSlugs([]string(msg), m.appGroup, m.environment)
		if len(slugs) == 0 {
			m.err = fmt.Errorf("no services matching convention {%s-*-%s} in cluster %s",
				m.appGroup, m.environment, m.cluster)
			return m, tea.Quit
		}
		m.serviceItems = slugs
		m.step = stepSelectService
		return m, m.fetchPreview(slugs[0])

	case previewMsg:
		m.previewLoading = false
		if msg.err == nil && msg.info != nil {
			m.previewCache[msg.slug] = msg.info
			if len(m.serviceItems) > 0 && msg.slug == m.serviceItems[m.serviceCursor] {
				m.currentPreview = msg.info
			}
		}
		return m, nil

	case tasksMsg:
		tasks := []cloud.TaskInfo(msg)
		if len(tasks) == 0 {
			m.err = fmt.Errorf("no running tasks for service %s — the service may be scaled to zero",
				m.service)
			return m, tea.Quit
		}
		if len(tasks) == 1 {
			t := tasks[0]
			m.taskARN = t.ARN
			m.taskShortID = t.ShortID
			if len(t.Containers) == 1 {
				m.container = t.Containers[0]
				return m, m.done()
			}
			m.containerItems = t.Containers
			m.step = stepSelectContainer
			return m, nil
		}
		m.taskItems = tasks
		m.step = stepSelectTask
		return m, nil

	case errMsg:
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Navigation / state helpers
// ---------------------------------------------------------------------------

// listNav handles up/down cursor movement and returns true on enter.
func listNav(msg tea.KeyMsg, cursor *int, count int) bool {
	switch msg.String() {
	case "up", "k":
		if *cursor > 0 {
			*cursor--
		}
	case "down", "j":
		if *cursor < count-1 {
			*cursor++
		}
	case "enter":
		return true
	}
	return false
}

// done builds the result and signals quit.
func (m *model) done() tea.Cmd {
	m.step = stepDone
	m.result = &Result{
		Profile:     m.client.Profile,
		Environment: m.environment,
		Cluster:     m.cluster,
		AppGroup:    m.appGroup,
		Service:     m.service,
		Slug:        m.slug,
		TaskARN:     m.taskARN,
		TaskShortID: m.taskShortID,
		Container:   m.container,
	}
	return tea.Quit
}

// ---------------------------------------------------------------------------
// Async commands (tea.Cmd)
// ---------------------------------------------------------------------------

func (m model) initClient() tea.Cmd {
	profile := m.profile
	region := m.region
	return func() tea.Msg {
		client, err := cloud.New(profile, region)
		if err != nil {
			return errMsg{fmt.Errorf("AWS client for profile %q: %w", profile, err)}
		}
		arn, err := client.CheckAuth(context.Background())
		if err != nil {
			return authErrMsg{err}
		}
		return clientReadyMsg{client: client, arn: arn}
	}
}

func (m model) checkAuth() tea.Cmd {
	return func() tea.Msg {
		arn, err := m.client.CheckAuth(context.Background())
		if err != nil {
			return authErrMsg{err}
		}
		return authOKMsg(arn)
	}
}

func (m model) loadClusters() tea.Cmd {
	return func() tea.Msg {
		clusters, err := m.client.ListClusters(context.Background())
		if err != nil {
			return errMsg{fmt.Errorf("listing clusters: %w", err)}
		}
		return clustersMsg(clusters)
	}
}

func (m model) loadServices() tea.Cmd {
	return func() tea.Msg {
		services, err := m.client.ListServices(context.Background(), m.cluster)
		if err != nil {
			return errMsg{fmt.Errorf("listing services: %w", err)}
		}
		return servicesMsg(services)
	}
}

func (m model) loadTasks() tea.Cmd {
	return func() tea.Msg {
		tasks, err := m.client.ListRunningTasks(context.Background(), m.cluster, m.service)
		if err != nil {
			return errMsg{fmt.Errorf("listing tasks: %w", err)}
		}
		return tasksMsg(tasks)
	}
}

func (m model) fetchPreview(slug string) tea.Cmd {
	appGroup := m.appGroup
	env := m.environment
	cluster := m.cluster
	client := m.client
	return func() tea.Msg {
		svcName := naming.SlugToServiceName(slug, appGroup, env)
		info, err := client.DescribeService(context.Background(), cluster, svcName)
		return previewMsg{slug: slug, info: info, err: err}
	}
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

func filterClusters(all []string, env string) []string {
	var out []string
	for _, c := range all {
		if naming.ClusterMatchesEnv(c, env) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out
}

func extractSlugs(services []string, appGroup, env string) []string {
	seen := map[string]bool{}
	hasWeb := false
	var others []string

	for _, svc := range services {
		if !naming.ServiceMatchesConvention(svc, appGroup, env) {
			continue
		}
		slug := naming.ServiceToSlug(svc, appGroup, env)
		if seen[slug] {
			continue
		}
		seen[slug] = true
		if slug == "web" {
			hasWeb = true
		} else {
			others = append(others, slug)
		}
	}
	sort.Strings(others)
	if hasWeb {
		return append([]string{"web"}, others...)
	}
	return others
}
