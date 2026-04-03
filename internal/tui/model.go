package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ecs-connect/internal/cloud"
	"ecs-connect/internal/config"
	"ecs-connect/internal/ddb"
	"ecs-connect/internal/naming"
	"ecs-connect/internal/recents"

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
	Client    *cloud.Client  // pre-authenticated client (built by main)
	Config    *config.Config // naming/env config (nil = generic mode)
	Cluster   string         // pre-selected cluster (skip picker)
	Service   string         // pre-selected service (skip picker)
	Container string         // pre-selected container (skip picker when task matches)
}

// Run launches the interactive TUI wizard and returns an outcome
// (ECS exec target or DynamoDB query result) plus the AWS client.
func Run(opts Options) (*Outcome, *cloud.Client, error) {
	m := newModel(opts)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
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
	return fm.outcome, fm.client, nil
}

// ---------------------------------------------------------------------------
// Steps
// ---------------------------------------------------------------------------

type step int

const (
	stepCheckAuth step = iota
	stepChooseBackend
	stepLoadDynamoClient
	stepSelectEnv
	stepLoadClusters
	stepSelectCluster
	stepLoadServices
	stepSelectService
	stepConfirm
	stepLoadTasks
	stepSelectTask
	stepSelectContainer
	stepDynamoPickKeyword
	stepLoadDynamoTables
	stepSelectDynamoTable
	stepLoadDynamoDescribe
	stepDynamoEnterPK
	stepDynamoEnterSK
	stepLoadDynamoQuery
	stepDynamoShowResults
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

	dynamoReadyMsg    struct{ c *ddb.Client }
	dynamoTablesMsg   []string
	dynamoDescribeMsg struct {
		schema *ddb.KeySchema
		err    error
	}
	dynamoQueryMsg struct {
		json string
		err  error
	}
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

	// task list preview (metadata from TaskInfo)
	taskPreviewViewport  viewport.Model
	taskPreviewScrollKey string

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

	// presetContainer is the CLI/YAML container name; applied when tasks load.
	presetContainer string

	dynamoMode          bool
	backendCursor       int
	ddbClient           *ddb.Client
	dynamoKeywordItems  []string
	dynamoKeywordCursor int
	dynamoTableItems    []string
	dynamoTableCursor   int
	dynamoTableName     string
	dynamoPKName        string
	dynamoPKType        string
	dynamoSKName        string
	dynamoSKType        string
	dynamoPKInput       textinput.Model
	dynamoSKInput       textinput.Model
	dynamoViewport      viewport.Model
	dynamoResultJSON    string

	cancelled bool
	err       error
	outcome   *Outcome
}

func newModel(opts Options) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ti := textinput.New()
	ti.Placeholder = "type 'yes' to continue"
	ti.CharLimit = 3

	pki := textinput.New()
	ski := textinput.New()

	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = false

	tvp := viewport.New(0, 0)
	tvp.MouseWheelEnabled = false

	dvp := viewport.New(0, 0)
	dvp.MouseWheelEnabled = true

	m := model{
		client:          opts.Client,
		cfg:             opts.Config,
		spinner:         s,
		confirmInput:    ti,
		dynamoPKInput:   pki,
		dynamoSKInput:   ski,
		previewCache:        make(map[string]*cloud.ServiceInfo),
		previewViewport:     vp,
		taskPreviewViewport: tvp,
		dynamoViewport:      dvp,
		profile:         opts.Client.Profile,
		cluster:         opts.Cluster,
		service:         opts.Service,
		presetContainer: strings.TrimSpace(opts.Container),
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
		if env := m.matchingConfiguredDefaultEnv(); env != "" {
			m.environment = env
			if m.shouldConfirm() {
				m.confirmInput.Focus()
				return stepConfirm, textinput.Blink
			}
			m.loadingMsg = "Loading clusters..."
			return stepLoadClusters, m.loadClusters()
		}
		return stepSelectEnv, nil
	}
	m.loadingMsg = "Loading clusters..."
	return stepLoadClusters, m.loadClusters()
}

func (m model) matchingConfiguredDefaultEnv() string {
	if m.cfg == nil || m.cfg.Defaults == nil {
		return ""
	}
	want := strings.TrimSpace(m.cfg.Defaults.Environment)
	if want == "" {
		return ""
	}
	for _, name := range m.envItems {
		if name == want {
			return want
		}
	}
	return ""
}

func defaultsBackend(cfg *config.Config) string {
	if cfg == nil || cfg.Defaults == nil {
		return ""
	}
	return cfg.Defaults.Backend
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
		stepSelectService, stepSelectTask, stepSelectContainer,
		stepSelectDynamoTable:
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
	case stepChooseBackend:
		if m.backendCursor > 1 {
			m.backendCursor = 1
		}
	case stepDynamoPickKeyword:
		if n := len(m.dynamoKeywordItems); m.dynamoKeywordCursor >= n {
			m.dynamoKeywordCursor = max(0, n-1)
		}
	case stepSelectDynamoTable:
		if n := len(m.applyFilter(m.dynamoTableItems)); m.dynamoTableCursor >= n {
			m.dynamoTableCursor = max(0, n-1)
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

func (m model) currentTaskForPreview() *cloud.TaskInfo {
	if len(m.taskItems) == 0 {
		return nil
	}
	labels := taskLabels(m.taskItems)
	_, indices := applyFilterWithIndices(labels, m.filterText)
	if len(indices) == 0 || m.taskCursor < 0 || m.taskCursor >= len(indices) {
		return nil
	}
	ti := m.taskItems[indices[m.taskCursor]]
	return &ti
}

func (m model) taskPreviewKey() string {
	t := m.currentTaskForPreview()
	if t == nil {
		return ""
	}
	return t.ARN
}

// syncTaskPreview updates the task metadata viewport on the task pick step.
func (m model) syncTaskPreview(resetScroll bool) model {
	if m.step != stepSelectTask {
		return m
	}
	w, h := previewViewportInnerSize(m)
	vp := m.taskPreviewViewport
	vp.Width = w
	vp.Height = h
	prevKey := m.taskPreviewScrollKey
	vp.SetContent(taskPreviewInnerContent(m))
	key := m.taskPreviewKey()
	if resetScroll || key != prevKey {
		vp.GotoTop()
		m.taskPreviewScrollKey = key
	}
	m.taskPreviewViewport = vp
	return m
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
	if m.step == stepSelectService {
		w, h := previewViewportInnerSize(m)
		vp := m.previewViewport
		vp.Width = w
		vp.Height = h
		m.previewViewport = vp
		return m
	}
	if m.step == stepSelectTask {
		w, h := previewViewportInnerSize(m)
		vp := m.taskPreviewViewport
		vp.Width = w
		vp.Height = h
		m.taskPreviewViewport = vp
		return m
	}
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

// tryPresetContainerForTask sets m.container when presetContainer matches the
// task's containers. Returns handled=true with done() or a quit-on-error cmd.
func (m *model) tryPresetContainerForTask(t cloud.TaskInfo) (tea.Cmd, bool) {
	want := strings.TrimSpace(m.presetContainer)
	if want == "" {
		return nil, false
	}
	for _, c := range t.Containers {
		if c == want {
			m.container = c
			return m.done(), true
		}
	}
	m.err = fmt.Errorf("no container %q in task %s — available: %v", want, t.ShortID, t.Containers)
	return tea.Quit, true
}

// tryWizardBack moves one step backward in the wizard when the user presses
// b (only when the list filter is inactive). Returns ok=true if handled.
func (m model) tryWizardBack() (model, tea.Cmd, bool) {
	switch m.step {
	case stepCheckAuth,
		stepLoadDynamoClient, stepLoadDynamoTables, stepLoadDynamoDescribe, stepLoadDynamoQuery,
		stepLoadClusters, stepLoadServices, stepLoadTasks:
		return m, nil, false

	case stepChooseBackend:
		m.cancelled = true
		return m, tea.Quit, true

	case stepSelectEnv:
		m.resetFilter()
		m.environment = ""
		if m.dynamoMode {
			m.ddbClient = nil
			m.dynamoMode = false
		}
		m.backendCursor = 0
		m.step = stepChooseBackend
		return m, nil, true

	case stepDynamoPickKeyword:
		m.resetFilter()
		m.environment = ""
		m.dynamoMode = false
		m.ddbClient = nil
		m.backendCursor = 0
		m.step = stepChooseBackend
		return m, nil, true

	case stepSelectDynamoTable:
		m.resetFilter()
		m.dynamoTableName = ""
		if m.useNaming() {
			m.environment = ""
			m.step = stepSelectEnv
			return m, nil, true
		}
		m.step = stepDynamoPickKeyword
		return m, nil, true

	case stepDynamoEnterPK:
		m.dynamoPKInput.Blur()
		m.dynamoPKInput.SetValue("")
		m.dynamoSKInput.SetValue("")
		m.step = stepSelectDynamoTable
		return m, nil, true

	case stepDynamoEnterSK:
		m.dynamoSKInput.Blur()
		m.dynamoSKInput.SetValue("")
		m.dynamoPKInput.Focus()
		m.step = stepDynamoEnterPK
		return m, textinput.Blink, true

	case stepDynamoShowResults:
		m.dynamoPKInput.Blur()
		m.dynamoSKInput.Blur()
		m.dynamoTableCursor = 0
		for i, t := range m.dynamoTableItems {
			if t == m.dynamoTableName {
				m.dynamoTableCursor = i
				break
			}
		}
		m.step = stepSelectDynamoTable
		return m, nil, true

	case stepConfirm:
		m.confirmInput.SetValue("")
		m.confirmInput.Blur()
		if m.dynamoMode {
			m.step = stepSelectEnv
			return m, nil, true
		}
		m.step = stepSelectService
		mp := &m
		cmd := mp.updateServicePreview()
		m = *mp
		m = m.syncPreviewForService(m.currentServiceKey(), true)
		return m, cmd, true

	case stepSelectCluster:
		m.resetFilter()
		m.cluster = ""
		m.appGroup = ""
		if m.useNaming() {
			m.environment = ""
			m.step = stepSelectEnv
			return m, nil, true
		}
		m.backendCursor = 0
		m.step = stepChooseBackend
		return m, nil, true

	case stepSelectService:
		m.resetFilter()
		m.slug = ""
		m.service = ""
		m.previewScrollKey = ""
		m.currentPreview = nil
		m.previewLoading = false
		if len(m.clusterItems) > 0 {
			m.step = stepSelectCluster
			return m, nil, true
		}
		m.loadingMsg = "Loading clusters..."
		m.step = stepLoadClusters
		return m, m.loadClusters(), true

	case stepSelectTask:
		m.resetFilter()
		m.taskARN = ""
		m.taskShortID = ""
		m.container = ""
		if len(m.serviceItems) > 0 {
			m.step = stepSelectService
			mp := &m
			cmd := mp.updateServicePreview()
			m = *mp
			m = m.syncPreviewForService(m.currentServiceKey(), true)
			return m, cmd, true
		}
		m.loadingMsg = "Loading services..."
		m.step = stepLoadServices
		return m, m.loadServices(), true

	case stepSelectContainer:
		m.resetFilter()
		m.container = ""
		m.step = stepSelectTask
		m = m.syncTaskPreview(true)
		return m, nil, true

	default:
		return m, nil, false
	}
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
		m = m.resizeDynamoViewport()
		return m, nil

	case tea.MouseMsg:
		if m.step == stepDynamoShowResults {
			var cmd tea.Cmd
			m.dynamoViewport, cmd = m.dynamoViewport.Update(msg)
			return m, cmd
		}
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
				if m.step == stepSelectTask {
					m = m.syncTaskPreview(true)
				}
				return m, nil
			}
			if m.step == stepDynamoShowResults {
				return m, tea.Quit
			}
			if m.step != stepConfirm {
				m.cancelled = true
				return m, tea.Quit
			}
		}

		if msg.String() == "b" && !m.filterActive {
			if nm, cmd, ok := m.tryWizardBack(); ok {
				return nm, cmd
			}
		}

		if m.step == stepDynamoShowResults && !m.filterActive {
			switch msg.String() {
			case "[":
				vp := m.dynamoViewport
				vp.ScrollUp(3)
				m.dynamoViewport = vp
				return m, nil
			case "]":
				vp := m.dynamoViewport
				vp.ScrollDown(3)
				m.dynamoViewport = vp
				return m, nil
			case "r":
				m.dynamoPKInput.SetValue("")
				m.dynamoSKInput.SetValue("")
				m.dynamoPKInput.Placeholder = fmt.Sprintf("%s (%s)", m.dynamoPKName, m.dynamoPKType)
				m.dynamoSKInput.Blur()
				m.dynamoPKInput.Focus()
				m.step = stepDynamoEnterPK
				return m, textinput.Blink
			case "e":
				if m.dynamoSKName != "" {
					m.dynamoPKInput.Blur()
					m.dynamoSKInput.Focus()
					m.step = stepDynamoEnterSK
				} else {
					m.dynamoSKInput.Blur()
					m.dynamoPKInput.Focus()
					m.step = stepDynamoEnterPK
				}
				return m, textinput.Blink
			case "c", "y":
				return m, dynamoCopyJSONCmd(m.dynamoResultJSON)
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

		if m.step == stepSelectTask && !m.filterActive {
			switch msg.String() {
			case "[":
				vp := m.taskPreviewViewport
				vp.ScrollUp(3)
				m.taskPreviewViewport = vp
				return m, nil
			case "]":
				vp := m.taskPreviewViewport
				vp.ScrollDown(3)
				m.taskPreviewViewport = vp
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
				if m.step == stepSelectTask {
					m = m.syncTaskPreview(true)
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
					if m.step == stepSelectTask {
						m = m.syncTaskPreview(true)
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
		case stepChooseBackend:
			if listNav(msg, &m.backendCursor, 2) {
				if m.backendCursor == 0 {
					m.dynamoMode = false
					next, cmd := m.afterAuth()
					m.step = next
					return m, cmd
				}
				m.dynamoMode = true
				m.loadingMsg = "Connecting to DynamoDB..."
				m.step = stepLoadDynamoClient
				return m, m.initDynamoClient()
			}

		case stepSelectEnv:
			visible := m.applyFilter(m.envItems)
			if listNav(msg, &m.envCursor, len(visible)) {
				if len(visible) > 0 && m.envCursor < len(visible) {
					m.environment = visible[m.envCursor]
					m.resetFilter()
					if m.dynamoMode {
						if m.shouldConfirm() {
							m.step = stepConfirm
							m.confirmInput.Focus()
							return m, textinput.Blink
						}
						m.loadingMsg = "Loading DynamoDB tables..."
						m.step = stepLoadDynamoTables
						return m, m.loadDynamoTables()
					}
					m.loadingMsg = "Loading clusters..."
					m.step = stepLoadClusters
					return m, m.loadClusters()
				}
			}

		case stepDynamoPickKeyword:
			if listNav(msg, &m.dynamoKeywordCursor, len(m.dynamoKeywordItems)) {
				if len(m.dynamoKeywordItems) > 0 && m.dynamoKeywordCursor < len(m.dynamoKeywordItems) {
					m.environment = m.dynamoKeywordItems[m.dynamoKeywordCursor]
					m.resetFilter()
					m.loadingMsg = "Loading DynamoDB tables..."
					m.step = stepLoadDynamoTables
					return m, m.loadDynamoTables()
				}
			}

		case stepSelectDynamoTable:
			visible := m.applyFilter(m.dynamoTableItems)
			if listNav(msg, &m.dynamoTableCursor, len(visible)) {
				if len(visible) > 0 && m.dynamoTableCursor < len(visible) {
					m.dynamoTableName = visible[m.dynamoTableCursor]
					m.resetFilter()
					m.loadingMsg = "Loading table keys..."
					m.step = stepLoadDynamoDescribe
					return m, m.loadDynamoKeySchema()
				}
			}

		case stepDynamoEnterPK:
			switch msg.String() {
			case "enter":
				if strings.TrimSpace(m.dynamoPKInput.Value()) == "" {
					return m, nil
				}
				if m.dynamoSKName != "" {
					m.dynamoSKInput.SetValue("")
					m.dynamoSKInput.Placeholder = fmt.Sprintf("%s (%s, optional)", m.dynamoSKName, m.dynamoSKType)
					m.dynamoPKInput.Blur()
					m.dynamoSKInput.Focus()
					m.step = stepDynamoEnterSK
					return m, textinput.Blink
				}
				m.loadingMsg = "Querying table..."
				m.step = stepLoadDynamoQuery
				return m, m.runDynamoQuery()
			default:
				var cmd tea.Cmd
				m.dynamoPKInput, cmd = m.dynamoPKInput.Update(msg)
				return m, cmd
			}

		case stepDynamoEnterSK:
			switch msg.String() {
			case "enter":
				m.loadingMsg = "Querying table..."
				m.step = stepLoadDynamoQuery
				return m, m.runDynamoQuery()
			default:
				var cmd tea.Cmd
				m.dynamoSKInput, cmd = m.dynamoSKInput.Update(msg)
				return m, cmd
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
					if m.dynamoMode {
						m.loadingMsg = "Loading DynamoDB tables..."
						m.step = stepLoadDynamoTables
						return m, m.loadDynamoTables()
					}
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
			prev := m.taskCursor
			if listNav(msg, &m.taskCursor, len(visible)) {
				if len(indices) > 0 && m.taskCursor < len(indices) {
					t := m.taskItems[indices[m.taskCursor]]
					m.taskARN = t.ARN
					m.taskShortID = t.ShortID
					m.resetFilter()
					m = m.syncTaskPreview(true)
					if len(t.Containers) == 1 {
						m.container = t.Containers[0]
						return m, m.done()
					}
					mp := &m
					if cmd, handled := mp.tryPresetContainerForTask(t); handled {
						return *mp, cmd
					}
					m = *mp
					m.containerItems = t.Containers
					m.step = stepSelectContainer
				}
			} else if m.taskCursor != prev && len(visible) > 0 {
				m = m.syncTaskPreview(true)
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
		if m.step == stepSelectTask {
			m = m.syncTaskPreview(false)
		}
		return m, cmd

	// --- async results ---

	case authOKMsg:
		m.authARN = string(msg)
		m.backendCursor = 0
		backend := config.NormalizeBackend(defaultsBackend(m.cfg))
		switch backend {
		case "dynamo":
			m.dynamoMode = true
			m.loadingMsg = "Connecting to DynamoDB..."
			m.step = stepLoadDynamoClient
			return m, m.initDynamoClient()
		case "ecs":
			m.dynamoMode = false
			next, cmd := m.afterAuth()
			m.step = next
			return m, cmd
		default:
			m.step = stepChooseBackend
			return m, nil
		}

	case dynamoReadyMsg:
		m.ddbClient = msg.c
		if m.useNaming() {
			if env := m.matchingConfiguredDefaultEnv(); env != "" {
				m.environment = env
				if m.shouldConfirm() {
					m.confirmInput.Focus()
					m.step = stepConfirm
					return m, textinput.Blink
				}
				m.loadingMsg = "Loading DynamoDB tables..."
				m.step = stepLoadDynamoTables
				return m, m.loadDynamoTables()
			}
			m.step = stepSelectEnv
			return m, nil
		}
		kw := ""
		if m.cfg != nil && m.cfg.Defaults != nil {
			kw = strings.TrimSpace(m.cfg.Defaults.DynamoKeyword)
		}
		if kw != "" {
			m.environment = kw
			m.loadingMsg = "Loading DynamoDB tables..."
			m.step = stepLoadDynamoTables
			return m, m.loadDynamoTables()
		}
		m.dynamoKeywordItems = []string{"staging", "production"}
		m.dynamoKeywordCursor = 0
		m.step = stepDynamoPickKeyword
		return m, nil

	case dynamoTablesMsg:
		filtered := ddb.FilterTablesByKeyword([]string(msg), m.environment)
		if len(filtered) == 0 {
			m.err = fmt.Errorf("no DynamoDB tables contain %q in their name", m.environment)
			return m, tea.Quit
		}
		m.dynamoTableItems = filtered
		wantTable := ""
		if m.cfg != nil && m.cfg.Defaults != nil {
			wantTable = strings.TrimSpace(m.cfg.Defaults.DynamoTable)
		}
		if wantTable != "" {
			for i, t := range filtered {
				if t == wantTable {
					m.dynamoTableName = wantTable
					m.dynamoTableCursor = i
					m.loadingMsg = "Loading table keys..."
					m.step = stepLoadDynamoDescribe
					return m, m.loadDynamoKeySchema()
				}
			}
		}
		m.dynamoTableCursor = 0
		m.step = stepSelectDynamoTable
		return m, nil

	case dynamoDescribeMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("describe table: %w", msg.err)
			return m, tea.Quit
		}
		ks := msg.schema
		m.dynamoPKName = ks.PartitionName
		m.dynamoPKType = ks.PartitionType
		m.dynamoSKName = ks.SortName
		m.dynamoSKType = ks.SortType
		m.dynamoPKInput.SetValue("")
		m.dynamoPKInput.Placeholder = fmt.Sprintf("%s (%s)", m.dynamoPKName, m.dynamoPKType)
		m.dynamoPKInput.Focus()
		m.step = stepDynamoEnterPK
		return m, textinput.Blink

	case dynamoQueryMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, tea.Quit
		}
		m.outcome = &Outcome{
			Mode: ModeDynamoDB,
			Dynamo: &DynamoOutcome{
				Table: m.dynamoTableName,
				JSON:  msg.json,
			},
		}
		m.dynamoResultJSON = msg.json
		m = m.syncDynamoResultViewport()
		m.step = stepDynamoShowResults
		return m, nil

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
		if want := strings.TrimSpace(m.cluster); want != "" {
			for _, c := range items {
				if c == want {
					m.cluster = c
					m.resetFilter()
					if m.useNaming() && m.environment != "" {
						m.appGroup = naming.AppGroup(m.cluster, m.environment)
					}
					m.loadingMsg = "Loading services..."
					m.step = stepLoadServices
					return m, m.loadServices()
				}
			}
		}
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
			if want := strings.TrimSpace(m.service); want != "" {
				for _, slug := range slugs {
					svcName := naming.SlugToServiceName(slug, m.appGroup, m.environment, m.defaultSlug())
					if svcName == want || slug == want {
						m.slug = slug
						m.service = svcName
						m.resetFilter()
						if m.shouldConfirm() {
							m.confirmInput.Focus()
							m.step = stepConfirm
							return m, textinput.Blink
						}
						m.loadingMsg = "Loading tasks..."
						m.step = stepLoadTasks
						return m, m.loadTasks()
					}
				}
			}
		} else {
			services := []string(msg)
			if len(services) == 0 {
				m.err = fmt.Errorf("no services found in cluster %s", m.cluster)
				return m, tea.Quit
			}
			m.serviceItems = services
			if want := strings.TrimSpace(m.service); want != "" {
				for _, s := range services {
					if s == want {
						m.service = s
						m.resetFilter()
						if m.shouldConfirm() {
							m.confirmInput.Focus()
							m.step = stepConfirm
							return m, textinput.Blink
						}
						m.loadingMsg = "Loading tasks..."
						m.step = stepLoadTasks
						return m, m.loadTasks()
					}
				}
			}
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
			mp := &m
			if cmd, handled := mp.tryPresetContainerForTask(t); handled {
				return *mp, cmd
			}
			m = *mp
			if len(t.Containers) == 1 {
				m.container = t.Containers[0]
				return m, m.done()
			}
			m.containerItems = t.Containers
			m.step = stepSelectContainer
			return m, nil
		}
		want := strings.TrimSpace(m.presetContainer)
		if want != "" {
			var match []cloud.TaskInfo
			for _, t := range tasks {
				for _, c := range t.Containers {
					if c == want {
						match = append(match, t)
						break
					}
				}
			}
			if len(match) == 1 {
				t := match[0]
				m.taskARN = t.ARN
				m.taskShortID = t.ShortID
				m.container = want
				return m, m.done()
			}
			if len(match) == 0 {
				m.err = fmt.Errorf("no running task in service %s has container %q", m.service, want)
				return m, tea.Quit
			}
		}
		m.taskItems = tasks
		m.step = stepSelectTask
		m = m.syncTaskPreview(true)
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
	m.outcome = &Outcome{
		Mode: ModeECS,
		ECS: &Result{
			Profile:     m.client.Profile,
			Environment: m.environment,
			Cluster:     m.cluster,
			AppGroup:    m.appGroup,
			Service:     m.service,
			Slug:        m.slug,
			TaskARN:     m.taskARN,
			TaskShortID: m.taskShortID,
			Container:   m.container,
		},
	}
	_ = recents.Save(m.client.Profile, recents.Target{
		Cluster:     m.cluster,
		Service:     m.service,
		TaskARN:     m.taskARN,
		TaskShortID: m.taskShortID,
		Container:   m.container,
		Environment: m.environment,
		AppGroup:    m.appGroup,
		Slug:        m.slug,
	})
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

func (m model) initDynamoClient() tea.Cmd {
	return func() tea.Msg {
		c, err := ddb.New(m.client.Profile, m.client.Region)
		if err != nil {
			return errMsg{err}
		}
		return dynamoReadyMsg{c: c}
	}
}

func (m model) loadDynamoTables() tea.Cmd {
	client := m.ddbClient
	return func() tea.Msg {
		all, err := client.ListTables(context.Background())
		if err != nil {
			return errMsg{fmt.Errorf("listing DynamoDB tables: %w", err)}
		}
		return dynamoTablesMsg(all)
	}
}

func (m model) loadDynamoKeySchema() tea.Cmd {
	tn := m.dynamoTableName
	client := m.ddbClient
	return func() tea.Msg {
		ks, err := client.DescribeKeySchema(context.Background(), tn)
		return dynamoDescribeMsg{schema: ks, err: err}
	}
}

func (m model) runDynamoQuery() tea.Cmd {
	skVal := strings.TrimSpace(m.dynamoSKInput.Value())
	skName, skType := m.dynamoSKName, m.dynamoSKType
	if skVal == "" {
		skName, skType, skVal = "", "", ""
	}
	in := ddb.QueryInput{
		Table:   m.dynamoTableName,
		PKName:  m.dynamoPKName,
		PKType:  m.dynamoPKType,
		PKValue: strings.TrimSpace(m.dynamoPKInput.Value()),
		SKName:  skName,
		SKType:  skType,
		SKValue: skVal,
	}
	client := m.ddbClient
	return func() tea.Msg {
		jsonStr, err := client.Query(context.Background(), in)
		return dynamoQueryMsg{json: jsonStr, err: err}
	}
}

func (m model) dynamoViewportInnerSize() (w, h int) {
	h = m.height - 16
	if h < 6 {
		h = 6
	}
	if m.width <= 0 {
		return 72, h
	}
	w = max(40, m.width-int(themeFrame().GetHorizontalFrameSize())-4)
	return w, h
}

func (m model) syncDynamoResultViewport() model {
	w, h := m.dynamoViewportInnerSize()
	vp := m.dynamoViewport
	vp.Width = w
	vp.Height = h
	vp.SetContent(m.dynamoResultJSON)
	m.dynamoViewport = vp
	return m
}

func (m model) resizeDynamoViewport() model {
	if m.step != stepDynamoShowResults {
		return m
	}
	return m.syncDynamoResultViewport()
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
