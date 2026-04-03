package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ecs-connect/internal/cloud"
	"ecs-connect/internal/config"
	"ecs-connect/internal/naming"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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
	Client  *cloud.Client  // pre-authenticated client (built by main)
	Config  *config.Config // naming/env config (nil = generic mode)
	Cluster string         // pre-selected cluster (skip picker)
	Service string         // pre-selected service (skip picker)
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
	stepCheckAuth step = iota
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

	clustersMsg []string
	servicesMsg []string
	tasksMsg    []cloud.TaskInfo

	previewMsg struct {
		key  string
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
	cfg    *config.Config

	width, height int

	spinner    spinner.Model
	loadingMsg string

	profile string

	// list-selection state
	envItems       []string
	clusterItems   []string
	serviceItems   []string // slugs (naming mode) or service names (generic)
	taskItems      []cloud.TaskInfo
	containerItems []string

	envCursor       int
	clusterCursor   int
	serviceCursor   int
	taskCursor      int
	containerCursor int

	// confirmation
	confirmInput textinput.Model

	// service preview
	previewCache     map[string]*cloud.ServiceInfo
	currentPreview   *cloud.ServiceInfo
	previewLoading   bool
	previewViewport  viewport.Model
	previewScrollKey string // resets viewport scroll when service selection changes

	// inline filter (activated with /)
	filterText   string
	filterActive bool

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

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = false

	m := model{
		client:          opts.Client,
		cfg:             opts.Config,
		spinner:         s,
		confirmInput:    ti,
		previewCache:    make(map[string]*cloud.ServiceInfo),
		previewViewport: vp,
		profile:         opts.Client.Profile,
		cluster:         opts.Cluster,
		service:         opts.Service,
		step:            stepCheckAuth,
	}

	if m.cfg.HasNaming() {
		for _, e := range m.cfg.Environments {
			m.envItems = append(m.envItems, e.Name)
		}
	}

	return m
}

// ---------------------------------------------------------------------------
// Mode helpers
// ---------------------------------------------------------------------------

func (m model) useNaming() bool {
	return m.cfg.HasNaming()
}

func (m model) defaultSlug() string {
	return m.cfg.GetDefaultSlug()
}

func (m model) shouldConfirm() bool {
	return m.cfg.ConfirmEnv(m.environment)
}

// afterAuth determines the next step once credentials are validated.
func (m *model) afterAuth() (step, tea.Cmd) {
	if m.cluster != "" && m.service != "" {
		m.loadingMsg = "Loading tasks..."
		return stepLoadTasks, m.loadTasks()
	}
	if m.cluster != "" {
		m.loadingMsg = "Loading services..."
		return stepLoadServices, m.loadServices()
	}
	if m.useNaming() {
		return stepSelectEnv, nil
	}
	m.loadingMsg = "Loading clusters..."
	return stepLoadClusters, m.loadClusters()
}

// ---------------------------------------------------------------------------
// Filter helpers
// ---------------------------------------------------------------------------

func (m *model) resetFilter() {
	m.filterText = ""
	m.filterActive = false
}

func (m model) applyFilter(items []string) []string {
	if m.filterText == "" {
		return items
	}
	lower := strings.ToLower(m.filterText)
	var out []string
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), lower) {
			out = append(out, item)
		}
	}
	return out
}

func (m model) isListStep() bool {
	switch m.step {
	case stepSelectEnv, stepSelectCluster,
		stepSelectService, stepSelectTask, stepSelectContainer:
		return true
	}
	return false
}

func (m *model) clampCurrentCursor() {
	switch m.step {
	case stepSelectEnv:
		if n := len(m.applyFilter(m.envItems)); m.envCursor >= n {
			m.envCursor = max(0, n-1)
		}
	case stepSelectCluster:
		if n := len(m.applyFilter(m.clusterItems)); m.clusterCursor >= n {
			m.clusterCursor = max(0, n-1)
		}
	case stepSelectService:
		if n := len(m.applyFilter(m.serviceItems)); m.serviceCursor >= n {
			m.serviceCursor = max(0, n-1)
		}
	case stepSelectTask:
		if n := len(m.applyFilter(taskLabels(m.taskItems))); m.taskCursor >= n {
			m.taskCursor = max(0, n-1)
		}
	case stepSelectContainer:
		if n := len(m.applyFilter(m.containerItems)); m.containerCursor >= n {
			m.containerCursor = max(0, n-1)
		}
	}
}

func (m model) currentServiceKey() string {
	visible := m.applyFilter(m.serviceItems)
	if m.serviceCursor >= 0 && m.serviceCursor < len(visible) {
		return visible[m.serviceCursor]
	}
	return ""
}

// syncPreviewForService updates the right-hand preview viewport. When resetScroll
// is false, vertical scroll is preserved (used while the loading spinner animates).
func (m model) syncPreviewForService(serviceKey string, resetScroll bool) model {
	if m.step != stepSelectService {
		return m
	}
	w, h := previewViewportInnerSize(m)
	vp := m.previewViewport
	vp.Width = w
	vp.Height = h
	prevKey := m.previewScrollKey
	vp.SetContent(previewInnerContent(m))
	if resetScroll || serviceKey != prevKey {
		vp.GotoTop()
		m.previewScrollKey = serviceKey
	}
	m.previewViewport = vp
	return m
}

func (m model) resizePreviewViewportOnly() model {
	if m.step != stepSelectService {
		return m
	}
	w, h := previewViewportInnerSize(m)
	vp := m.previewViewport
	vp.Width = w
	vp.Height = h
	m.previewViewport = vp
	return m
}

func (m *model) updateServicePreview() tea.Cmd {
	visible := m.applyFilter(m.serviceItems)
	if len(visible) == 0 || m.serviceCursor >= len(visible) {
		m.currentPreview = nil
		m.previewLoading = false
		return nil
	}
	key := visible[m.serviceCursor]
	if cached, ok := m.previewCache[key]; ok {
		m.currentPreview = cached
		m.previewLoading = false
		return nil
	}
	m.currentPreview = nil
	m.previewLoading = true
	return m.fetchPreview(key)
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m model) Init() tea.Cmd {
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
		m = m.resizePreviewViewportOnly()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.cancelled = true
			return m, tea.Quit
		}

		if msg.String() == "esc" {
			if m.filterActive {
				m.resetFilter()
				m.clampCurrentCursor()
				if m.step == stepSelectService {
					var cmd tea.Cmd
					cmd = m.updateServicePreview()
					m = m.syncPreviewForService(m.currentServiceKey(), true)
					return m, cmd
				}
				return m, nil
			}
			if m.step != stepConfirm {
				m.cancelled = true
				return m, tea.Quit
			}
		}

		// Filter text input: printable chars and backspace while filter is
		// active in a list step. Arrow keys and enter fall through to the
		// step-specific handlers below.
		if m.step == stepSelectService && !m.filterActive {
			switch msg.String() {
			case "[":
				vp := m.previewViewport
				vp.ScrollUp(3)
				m.previewViewport = vp
				return m, nil
			case "]":
				vp := m.previewViewport
				vp.ScrollDown(3)
				m.previewViewport = vp
				return m, nil
			}
		}

		if m.filterActive && m.isListStep() {
			key := msg.String()
			switch key {
			case "backspace":
				if len(m.filterText) > 0 {
					m.filterText = m.filterText[:len(m.filterText)-1]
				}
				if m.filterText == "" {
					m.filterActive = false
				}
				m.clampCurrentCursor()
				if m.step == stepSelectService {
					cmd := m.updateServicePreview()
					m = m.syncPreviewForService(m.currentServiceKey(), true)
					return m, cmd
				}
				return m, nil
			case "up", "down", "enter":
				// fall through to step-specific handlers
			default:
				if len(key) == 1 && key[0] >= 32 && key[0] <= 126 {
					m.filterText += key
					m.clampCurrentCursor()
					if m.step == stepSelectService {
						cmd := m.updateServicePreview()
						m = m.syncPreviewForService(m.currentServiceKey(), true)
						return m, cmd
					}
				}
				return m, nil
			}
		}

		// Activate filter with /
		if !m.filterActive && m.isListStep() && msg.String() == "/" {
			m.filterActive = true
			m.filterText = ""
			return m, nil
		}

		switch m.step {
		case stepSelectEnv:
			visible := m.applyFilter(m.envItems)
			if listNav(msg, &m.envCursor, len(visible)) {
				if len(visible) > 0 && m.envCursor < len(visible) {
					m.environment = visible[m.envCursor]
					m.resetFilter()
					m.loadingMsg = "Loading clusters..."
					m.step = stepLoadClusters
					return m, m.loadClusters()
				}
			}

		case stepSelectCluster:
			visible := m.applyFilter(m.clusterItems)
			if listNav(msg, &m.clusterCursor, len(visible)) {
				if len(visible) > 0 && m.clusterCursor < len(visible) {
					m.cluster = visible[m.clusterCursor]
					m.resetFilter()
					if m.useNaming() && m.environment != "" {
						m.appGroup = naming.AppGroup(m.cluster, m.environment)
					}
					m.loadingMsg = "Loading services..."
					m.step = stepLoadServices
					return m, m.loadServices()
				}
			}

		case stepSelectService:
			visible := m.applyFilter(m.serviceItems)
			prev := m.serviceCursor
			switch msg.String() {
			case "up", "k":
				if m.serviceCursor > 0 {
					m.serviceCursor--
				}
			case "down", "j":
				if m.serviceCursor < len(visible)-1 {
					m.serviceCursor++
				}
			case "enter":
				if len(visible) > 0 && m.serviceCursor < len(visible) {
					selected := visible[m.serviceCursor]
					if m.useNaming() && m.appGroup != "" {
						m.slug = selected
						m.service = naming.SlugToServiceName(m.slug, m.appGroup, m.environment, m.defaultSlug())
					} else {
						m.service = selected
					}
					m.resetFilter()
					if m.shouldConfirm() {
						m.step = stepConfirm
						m.confirmInput.Focus()
						return m, textinput.Blink
					}
					m.loadingMsg = "Loading tasks..."
					m.step = stepLoadTasks
					return m, m.loadTasks()
				}
			}
			if m.serviceCursor != prev && len(visible) > 0 && m.serviceCursor < len(visible) {
				key := visible[m.serviceCursor]
				if cached, ok := m.previewCache[key]; ok {
					m.currentPreview = cached
					m.previewLoading = false
				} else {
					m.currentPreview = nil
					m.previewLoading = true
					m = m.syncPreviewForService(key, true)
					return m, m.fetchPreview(key)
				}
				m = m.syncPreviewForService(key, true)
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
			labels := taskLabels(m.taskItems)
			visible, indices := applyFilterWithIndices(labels, m.filterText)
			if listNav(msg, &m.taskCursor, len(visible)) {
				if len(indices) > 0 && m.taskCursor < len(indices) {
					t := m.taskItems[indices[m.taskCursor]]
					m.taskARN = t.ARN
					m.taskShortID = t.ShortID
					m.resetFilter()
					if len(t.Containers) == 1 {
						m.container = t.Containers[0]
						return m, m.done()
					}
					m.containerItems = t.Containers
					m.step = stepSelectContainer
				}
			}

		case stepSelectContainer:
			visible := m.applyFilter(m.containerItems)
			if listNav(msg, &m.containerCursor, len(visible)) {
				if len(visible) > 0 && m.containerCursor < len(visible) {
					m.container = visible[m.containerCursor]
					m.resetFilter()
					return m, m.done()
				}
			}
		}

	// spinner animation
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		if m.step == stepSelectService && m.previewLoading {
			m = m.syncPreviewForService(m.currentServiceKey(), false)
		}
		return m, cmd

	// --- async results ---

	case authOKMsg:
		m.authARN = string(msg)
		next, cmd := m.afterAuth()
		m.step = next
		return m, cmd

	case authErrMsg:
		m.err = fmt.Errorf(
			"not authenticated — run:\n\n  aws sso login --profile %s", m.profile)
		return m, tea.Quit

	case clustersMsg:
		var items []string
		if m.useNaming() && m.environment != "" {
			items = filterClusters([]string(msg), m.environment)
			if len(items) == 0 {
				m.err = fmt.Errorf("no ECS clusters found ending with -%s", m.environment)
				return m, tea.Quit
			}
		} else {
			items = []string(msg)
			if len(items) == 0 {
				m.err = fmt.Errorf("no ECS clusters found")
				return m, tea.Quit
			}
		}
		m.clusterItems = items
		m.step = stepSelectCluster
		return m, nil

	case servicesMsg:
		if m.useNaming() && m.appGroup != "" {
			slugs := extractSlugs([]string(msg), m.appGroup, m.environment, m.defaultSlug())
			if len(slugs) == 0 {
				m.err = fmt.Errorf("no services matching convention {%s-*-%s} in cluster %s",
					m.appGroup, m.environment, m.cluster)
				return m, tea.Quit
			}
			m.serviceItems = slugs
		} else {
			services := []string(msg)
			if len(services) == 0 {
				m.err = fmt.Errorf("no services found in cluster %s", m.cluster)
				return m, tea.Quit
			}
			m.serviceItems = services
		}
		m.step = stepSelectService
		m.serviceCursor = 0
		visible := m.applyFilter(m.serviceItems)
		key := visible[0]
		m = m.syncPreviewForService(key, true)
		return m, m.fetchPreview(key)

	case previewMsg:
		m.previewLoading = false
		if msg.err == nil && msg.info != nil {
			m.previewCache[msg.key] = msg.info
			if len(m.serviceItems) > 0 {
				visible := m.applyFilter(m.serviceItems)
				if m.serviceCursor < len(visible) && msg.key == visible[m.serviceCursor] {
					m.currentPreview = msg.info
				}
			}
		}
		if m.step == stepSelectService {
			m = m.syncPreviewForService(m.currentServiceKey(), true)
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
// Navigation helpers
// ---------------------------------------------------------------------------

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

func (m model) fetchPreview(key string) tea.Cmd {
	cluster := m.cluster
	client := m.client

	var svcName string
	if m.useNaming() && m.appGroup != "" {
		svcName = naming.SlugToServiceName(key, m.appGroup, m.environment, m.defaultSlug())
	} else {
		svcName = key
	}

	return func() tea.Msg {
		info, err := client.DescribeService(context.Background(), cluster, svcName)
		return previewMsg{key: key, info: info, err: err}
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

func extractSlugs(services []string, appGroup, env, defaultSlug string) []string {
	seen := map[string]bool{}
	hasDefault := false
	var others []string

	for _, svc := range services {
		if !naming.ServiceMatchesConvention(svc, appGroup, env) {
			continue
		}
		slug := naming.ServiceToSlug(svc, appGroup, env, defaultSlug)
		if seen[slug] {
			continue
		}
		seen[slug] = true
		if slug == defaultSlug {
			hasDefault = true
		} else {
			others = append(others, slug)
		}
	}
	sort.Strings(others)
	if hasDefault {
		return append([]string{defaultSlug}, others...)
	}
	return others
}

func applyFilterWithIndices(items []string, filterText string) ([]string, []int) {
	if filterText == "" {
		indices := make([]int, len(items))
		for i := range items {
			indices[i] = i
		}
		return items, indices
	}
	lower := strings.ToLower(filterText)
	var filtered []string
	var indices []int
	for i, item := range items {
		if strings.Contains(strings.ToLower(item), lower) {
			filtered = append(filtered, item)
			indices = append(indices, i)
		}
	}
	return filtered, indices
}
