package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"ecs-connect/internal/cloud"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Color / theme (NO_COLOR aware)
// ---------------------------------------------------------------------------

var noColor = os.Getenv("NO_COLOR") != ""

func themeTitle() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if !noColor {
		s = s.Foreground(lipgloss.Color("39"))
	}
	return s
}

func themeSelected() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if !noColor {
		s = s.Foreground(lipgloss.Color("230")).Background(lipgloss.Color("57"))
	}
	return s
}

func themeNormal() lipgloss.Style {
	s := lipgloss.NewStyle()
	if !noColor {
		s = s.Foreground(lipgloss.Color("252"))
	}
	return s
}

func themeDim() lipgloss.Style {
	s := lipgloss.NewStyle()
	if !noColor {
		s = s.Foreground(lipgloss.Color("242"))
	}
	return s
}

func themeSuccess() lipgloss.Style {
	s := lipgloss.NewStyle()
	if !noColor {
		s = s.Foreground(lipgloss.Color("82"))
	}
	return s
}

func themeWarning() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if !noColor {
		s = s.Foreground(lipgloss.Color("196"))
	}
	return s
}

func themePreviewBorder() lipgloss.Style {
	s := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1)
	if !noColor {
		s = s.BorderForeground(lipgloss.Color("62"))
	}
	return s
}

func themeBreadcrumbPast() lipgloss.Style {
	s := lipgloss.NewStyle()
	if !noColor {
		s = s.Foreground(lipgloss.Color("245"))
	}
	return s
}

func themeBreadcrumbHere() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if !noColor {
		s = s.Foreground(lipgloss.Color("141"))
	}
	return s
}

func themeSep() lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
}

func themeProgressDone() lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
}

func themeProgressCurrent() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if !noColor {
		s = s.Foreground(lipgloss.Color("39"))
	}
	return s
}

func themeProgressTodo() lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
}

func themeFooter() lipgloss.Style {
	s := lipgloss.NewStyle()
	if !noColor {
		s = s.Background(lipgloss.Color("236")).Foreground(lipgloss.Color("248"))
	}
	return s
}

func themeFrame() lipgloss.Style {
	s := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1)
	if !noColor {
		s = s.BorderForeground(lipgloss.Color("237"))
	}
	return s
}

func themeZebraA() lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Background(lipgloss.Color("235")).Foreground(lipgloss.Color("252"))
}

func themeZebraB() lipgloss.Style {
	if noColor {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Background(lipgloss.Color("233")).Foreground(lipgloss.Color("252"))
}

func themeMatch() lipgloss.Style {
	s := lipgloss.NewStyle().Bold(true)
	if !noColor {
		s = s.Foreground(lipgloss.Color("220"))
	}
	return s
}

var (
	inProgressStyle = lipgloss.NewStyle()
)

func init() {
	if !noColor {
		inProgressStyle = inProgressStyle.Foreground(lipgloss.Color("220"))
	}
}

// ---------------------------------------------------------------------------
// Layout
// ---------------------------------------------------------------------------

func listColumnWidth(m model) int {
	if m.width <= 0 {
		return 42
	}
	if m.step == stepSelectService || m.step == stepSelectTask {
		const gap = 4
		previewMin := 28
		listMin := 26
		listW := min(m.width*48/100, 54)
		if listW < listMin {
			listW = listMin
		}
		previewOuter := m.width - listW - gap
		if previewOuter < previewMin {
			previewOuter = previewMin
			listW = m.width - previewOuter - gap
			if listW < listMin {
				listW = listMin
			}
		}
		return listW
	}
	return min(m.width-6, 58)
}

// previewPaneSize returns the outer width (including border) and content height
// budget for the preview box (viewport + chrome).
func previewPaneSize(m model) (outerW, contentH int) {
	margin := 9
	contentH = m.height - margin
	if contentH < 5 {
		contentH = 5
	}
	if m.width <= 0 {
		return 34, contentH
	}
	listW := listColumnWidth(m)
	outerW = m.width - listW - 4
	if outerW < 28 {
		outerW = 28
	}
	return outerW, contentH
}

// previewViewportInnerSize is the width/height passed to the bubbles viewport model.
func previewViewportInnerSize(m model) (iw, ih int) {
	outerW, h := previewPaneSize(m)
	b := themePreviewBorder()
	iw = max(12, outerW-b.GetHorizontalFrameSize())
	ih = max(4, h-b.GetVerticalFrameSize())
	return iw, ih
}

func previewInnerContent(m model) string {
	if m.previewLoading {
		return fmt.Sprintf("%s Loading...", m.spinner.View())
	}
	if m.currentPreview != nil {
		return formatServiceInfo(m.currentPreview)
	}
	return themeDim().Render("No preview")
}

func taskPreviewInnerContent(m model) string {
	t := m.currentTaskForPreview()
	if t == nil {
		return themeDim().Render("No preview")
	}
	return formatTaskInfo(t)
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m model) View() string {
	var body strings.Builder

	if labels, current, ok := m.wizardProgress(); ok {
		body.WriteString(renderProgress(labels, current))
	}

	switch m.step {
	case stepCheckAuth:
		body.WriteString(m.breadcrumb())
		body.WriteString(fmt.Sprintf("\n  %s Checking AWS credentials...\n", m.spinner.View()))

	case stepChooseBackend:
		body.WriteString(m.breadcrumb())
		backends := []string{"ECS Exec (containers)", "DynamoDB (query tables)"}
		body.WriteString(m.renderList("Choose backend", backends, m.backendCursor))

	case stepLoadDynamoClient:
		body.WriteString(m.breadcrumb())
		body.WriteString(fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.loadingMsg))

	case stepSelectEnv:
		body.WriteString(m.breadcrumb())
		body.WriteString(m.renderList("Select Environment", m.applyFilter(m.envItems), m.envCursor))

	case stepDynamoPickKeyword:
		body.WriteString(m.breadcrumb())
		body.WriteString(m.renderList("Filter tables by name (keyword)", m.dynamoKeywordItems, m.dynamoKeywordCursor))

	case stepLoadDynamoTables, stepLoadDynamoDescribe, stepLoadDynamoQuery:
		body.WriteString(m.breadcrumb())
		body.WriteString(fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.loadingMsg))

	case stepSelectDynamoTable:
		body.WriteString(m.breadcrumb())
		body.WriteString(m.renderList("Select DynamoDB table", m.applyFilter(m.dynamoTableItems), m.dynamoTableCursor))

	case stepDynamoEnterPK:
		body.WriteString(m.breadcrumb())
		body.WriteString("\n")
		body.WriteString(themeTitle().Render("  Partition key"))
		body.WriteString("\n\n  ")
		body.WriteString(m.dynamoPKInput.View())
		body.WriteString("\n")

	case stepDynamoEnterSK:
		body.WriteString(m.breadcrumb())
		body.WriteString("\n")
		body.WriteString(themeTitle().Render("  Sort key (optional)"))
		body.WriteString("\n\n  ")
		body.WriteString(m.dynamoSKInput.View())
		body.WriteString("\n")

	case stepDynamoShowResults:
		body.WriteString(m.breadcrumb())
		body.WriteString("\n")
		body.WriteString(themeTitle().Render("  Query results"))
		body.WriteString("\n\n")
		body.WriteString(m.dynamoViewport.View())

	case stepLoadClusters, stepLoadServices, stepLoadTasks:
		body.WriteString(m.breadcrumb())
		body.WriteString(fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.loadingMsg))

	case stepSelectCluster:
		body.WriteString(m.breadcrumb())
		body.WriteString(m.renderList("Select Cluster", m.applyFilter(m.clusterItems), m.clusterCursor))

	case stepSelectService:
		body.WriteString(m.breadcrumb())
		visible := m.applyFilter(m.serviceItems)
		list := m.renderList("Select Service", visible, m.serviceCursor)
		preview := m.renderPreviewPanel()
		body.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", preview))

	case stepConfirm:
		body.WriteString(m.breadcrumb())
		body.WriteString(m.renderConfirm())

	case stepSelectTask:
		body.WriteString(m.breadcrumb())
		labels := taskLabels(m.taskItems)
		visible, _ := applyFilterWithIndices(labels, m.filterText)
		list := m.renderList("Select Task", visible, m.taskCursor)
		preview := m.renderTaskPreviewPanel()
		body.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", preview))

	case stepSelectContainer:
		body.WriteString(m.breadcrumb())
		body.WriteString(m.renderList("Select Container", m.applyFilter(m.containerItems), m.containerCursor))
	}

	body.WriteString(m.renderFooter())

	out := body.String()
	if m.width > 0 {
		out = themeFrame().Width(m.width).Render(out)
	}
	return out
}

func (m model) wizardProgress() (labels []string, current int, ok bool) {
	switch m.step {
	case stepCheckAuth, stepChooseBackend, stepLoadDynamoClient:
		return nil, 0, false
	}
	if m.dynamoMode {
		if m.useNaming() {
			labels = []string{"Env", "Table", "Query"}
			switch m.step {
			case stepSelectEnv, stepConfirm, stepLoadDynamoTables:
				return labels, 0, true
			case stepSelectDynamoTable, stepLoadDynamoDescribe:
				return labels, 1, true
			case stepDynamoEnterPK, stepDynamoEnterSK, stepLoadDynamoQuery, stepDynamoShowResults:
				return labels, 2, true
			}
			return labels, 0, true
		}
		labels = []string{"Keyword", "Table", "Query"}
		switch m.step {
		case stepDynamoPickKeyword, stepLoadDynamoTables:
			return labels, 0, true
		case stepSelectDynamoTable, stepLoadDynamoDescribe:
			return labels, 1, true
		case stepDynamoEnterPK, stepDynamoEnterSK, stepLoadDynamoQuery, stepDynamoShowResults:
			return labels, 2, true
		}
		return labels, 0, true
	}
	if m.useNaming() {
		labels = []string{"Env", "Cluster", "Service", "Task"}
		switch m.step {
		case stepSelectEnv:
			return labels, 0, true
		case stepLoadClusters, stepSelectCluster:
			return labels, 1, true
		case stepLoadServices, stepSelectService, stepConfirm:
			return labels, 2, true
		case stepLoadTasks, stepSelectTask, stepSelectContainer:
			return labels, 3, true
		}
		return labels, 0, true
	}
	labels = []string{"Cluster", "Service", "Task"}
	switch m.step {
	case stepLoadClusters, stepSelectCluster:
		return labels, 0, true
	case stepLoadServices, stepSelectService, stepConfirm:
		return labels, 1, true
	case stepLoadTasks, stepSelectTask, stepSelectContainer:
		return labels, 2, true
	}
	return labels, 0, true
}

func renderProgress(labels []string, current int) string {
	var parts []string
	for i, lb := range labels {
		var st lipgloss.Style
		switch {
		case i < current:
			st = themeProgressDone()
		case i == current:
			st = themeProgressCurrent()
		default:
			st = themeProgressTodo()
		}
		marker := "◯"
		if i < current {
			marker = "◉"
		} else if i == current {
			marker = "◎"
		}
		parts = append(parts, st.Render(fmt.Sprintf("%s %s", marker, lb)))
	}
	line := "  " + strings.Join(parts, themeSep().Render("  ·  ")) + "\n"
	dim := themeDim()
	under := strings.Repeat("─", min(max(20, len(line)-2), 56))
	return line + dim.Render("  "+under) + "\n\n"
}

func (m model) renderFooter() string {
	var hints []string
	if m.filterActive {
		hints = []string{"↑/↓ navigate", "enter select", "esc clear filter"}
	} else if m.step == stepChooseBackend {
		hints = []string{"↑/↓ navigate", "enter select", "b exit", "esc cancel"}
	} else if m.step == stepDynamoShowResults {
		hints = []string{"wheel scroll", "[/] scroll", "c/y copy JSON", "e edit keys", "r new query", "b back", "esc exit"}
	} else if m.step == stepDynamoEnterPK {
		hints = []string{"enter submit", "b back", "esc cancel"}
	} else if m.step == stepDynamoEnterSK {
		hints = []string{"enter run query", "leave empty to omit sort condition", "b back", "esc cancel"}
	} else if m.step == stepConfirm {
		hints = []string{"type yes + enter", "b back", "esc cancel"}
	} else if m.step == stepSelectService {
		hints = []string{"↑/↓ navigate", "enter select", "/ filter", "[/] preview scroll", "b back", "esc cancel"}
	} else if m.step == stepSelectTask {
		hints = []string{"↑/↓ navigate", "enter select", "/ filter", "[/] task details scroll", "b back", "esc cancel"}
	} else {
		hints = []string{"↑/↓ navigate", "enter select", "/ filter", "b back", "esc cancel"}
	}
	line := "  " + strings.Join(hints, "  ·  ")
	if m.width > 8 {
		return "\n" + themeFooter().Width(max(0, m.width-2)).Padding(0, 1).Render(line) + "\n"
	}
	return "\n" + themeFooter().Render(line) + "\n"
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

func (m model) breadcrumb() string {
	parts := m.breadcrumbPath()
	if len(parts) == 0 {
		return "\n"
	}
	sep := themeSep().Render(" → ")
	var b strings.Builder
	b.WriteString("\n  ")
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		if i < len(parts)-1 {
			b.WriteString(themeBreadcrumbPast().Render(p))
		} else {
			b.WriteString(themeBreadcrumbHere().Render(p))
		}
	}
	b.WriteString("\n")
	return b.String()
}

func (m model) breadcrumbPath() []string {
	var parts []string
	if m.profile != "" {
		parts = append(parts, m.profile)
	}
	if m.dynamoMode {
		if m.environment != "" {
			parts = append(parts, m.environment)
		}
		if m.dynamoTableName != "" {
			parts = append(parts, m.dynamoTableName)
		}
		return parts
	}
	if m.environment != "" {
		parts = append(parts, m.environment)
	}
	if m.cluster != "" {
		parts = append(parts, m.cluster)
	}
	if m.slug != "" {
		parts = append(parts, m.slug)
	} else if m.service != "" {
		parts = append(parts, m.service)
	}
	return parts
}

func (m model) renderList(title string, items []string, cursor int) string {
	colW := listColumnWidth(m)
	var b strings.Builder
	b.WriteString("\n")
	titleLine := themeTitle().Width(colW).Render("  " + title)
	b.WriteString(titleLine)
	b.WriteString("\n")

	if m.filterActive && m.filterText != "" {
		b.WriteString(themeDim().Render("  filter: ") + m.filterText)
		b.WriteString("\n")
	}

	b.WriteString("\n")

	if len(items) == 0 {
		b.WriteString(themeDim().Render("    (no matches)"))
		b.WriteString("\n")
		return b.String()
	}

	maxVisible := m.height - 12
	if maxVisible < 5 {
		maxVisible = 5
	}
	start, end := visibleWindow(len(items), cursor, maxVisible)

	if start > 0 {
		b.WriteString(themeDim().Render("    ↑ more"))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		prefix := "    "
		rowStyle := themeNormal()
		var line string
		if i == cursor {
			prefix = "  ▸ "
			rowStyle = themeSelected()
			line = items[i]
			if m.filterActive && m.filterText != "" {
				line = highlightFilterPlain(line, m.filterText)
			}
		} else {
			line = items[i]
			if m.filterActive && m.filterText != "" {
				line = highlightFilter(line, m.filterText)
			}
			if i%2 == 1 {
				rowStyle = themeZebraA()
			} else {
				rowStyle = themeZebraB()
			}
		}
		padded := rowStyle.Width(colW).Render(prefix + line)
		b.WriteString(padded)
		b.WriteString("\n")
	}
	if end < len(items) {
		b.WriteString(themeDim().Render("    ↓ more"))
		b.WriteString("\n")
	}
	return b.String()
}

func highlightFilter(line, needle string) string {
	if needle == "" {
		return line
	}
	lowerLine := strings.ToLower(line)
	lowerNeedle := strings.ToLower(needle)
	idx := strings.Index(lowerLine, lowerNeedle)
	if idx < 0 {
		return line
	}
	pre := line[:idx]
	mat := line[idx : idx+len(needle)]
	post := line[idx+len(needle):]
	return pre + themeMatch().Render(mat) + post
}

func highlightFilterPlain(line, needle string) string {
	if needle == "" {
		return line
	}
	lowerLine := strings.ToLower(line)
	lowerNeedle := strings.ToLower(needle)
	idx := strings.Index(lowerLine, lowerNeedle)
	if idx < 0 {
		return line
	}
	pre := line[:idx]
	mat := line[idx : idx+len(needle)]
	post := line[idx+len(needle):]
	return pre + themeMatch().Render(mat) + post
}

func (m model) renderPreviewPanel() string {
	vpView := m.previewViewport.View()
	scrollHint := ""
	lines := m.previewViewport.TotalLineCount()
	vh := m.previewViewport.Height
	if vh > 0 && lines > vh {
		if !m.previewViewport.AtBottom() {
			below := lines - m.previewViewport.YOffset - vh
			if below < 0 {
				below = 0
			}
			scrollHint = themeDim().Render(fmt.Sprintf("\n  %d lines below · ]", below))
		} else if !m.previewViewport.AtTop() {
			scrollHint = themeDim().Render("\n  [ = scroll up")
		}
	}
	return themePreviewBorder().Render(vpView) + scrollHint
}

func (m model) renderTaskPreviewPanel() string {
	vpView := m.taskPreviewViewport.View()
	scrollHint := ""
	lines := m.taskPreviewViewport.TotalLineCount()
	vh := m.taskPreviewViewport.Height
	if vh > 0 && lines > vh {
		if !m.taskPreviewViewport.AtBottom() {
			below := lines - m.taskPreviewViewport.YOffset - vh
			if below < 0 {
				below = 0
			}
			scrollHint = themeDim().Render(fmt.Sprintf("\n  %d lines below · ]", below))
		} else if !m.taskPreviewViewport.AtTop() {
			scrollHint = themeDim().Render("\n  [ = scroll up")
		}
	}
	return themePreviewBorder().Render(vpView) + scrollHint
}

func formatServiceInfo(info *cloud.ServiceInfo) string {
	var b strings.Builder
	b.WriteString(themeTitle().Render("Service Details"))
	b.WriteString("\n\n")

	status := info.Status
	if status == "ACTIVE" {
		status = themeSuccess().Render(status)
	} else {
		status = themeWarning().Render(status)
	}
	b.WriteString(fmt.Sprintf("  Status    %s\n", status))
	b.WriteString(fmt.Sprintf("  Desired   %d\n", info.DesiredCount))

	runStr := fmt.Sprintf("%d", info.RunningCount)
	if info.RunningCount == info.DesiredCount {
		runStr = themeSuccess().Render(runStr)
	} else {
		runStr = themeWarning().Render(runStr)
	}
	b.WriteString(fmt.Sprintf("  Running   %s\n", runStr))
	b.WriteString(fmt.Sprintf("  Pending   %d\n", info.PendingCount))
	b.WriteString(fmt.Sprintf("  TaskDef   %s\n", info.TaskDef))

	if len(info.Deployments) > 0 {
		b.WriteString("\n")
		b.WriteString(themeTitle().Render("Recent Deployments"))
		b.WriteString("\n\n")
		for _, d := range info.Deployments {
			b.WriteString(formatDeploymentLine(d))
			b.WriteString("\n")
		}
	}

	return b.String()
}

func formatTaskInfo(t *cloud.TaskInfo) string {
	var b strings.Builder
	b.WriteString(themeTitle().Render("Task Details"))
	b.WriteString("\n\n")

	st := t.Status
	if strings.EqualFold(st, "RUNNING") {
		st = themeSuccess().Render(st)
	} else {
		st = themeWarning().Render(st)
	}
	b.WriteString(fmt.Sprintf("  Status    %s\n", st))

	cpu := t.CPU
	if cpu == "" {
		cpu = "—"
	}
	mem := t.Memory
	if mem == "" {
		mem = "—"
	}
	b.WriteString(fmt.Sprintf("  CPU       %s\n", cpu))
	b.WriteString(fmt.Sprintf("  Memory    %s\n", mem))

	pf := t.PlatformFamily
	if pf == "" {
		pf = "—"
	}
	pv := t.PlatformVersion
	if pv == "" {
		pv = "—"
	}
	b.WriteString(fmt.Sprintf("  Platform  %s\n", pf))
	b.WriteString(fmt.Sprintf("  Plat. ver %s\n", pv))
	lt := t.LaunchType
	if lt == "" {
		lt = "—"
	}
	b.WriteString(fmt.Sprintf("  Launch    %s\n", lt))

	started := "—"
	if !t.StartedAt.IsZero() {
		started = t.StartedAt.Format(time.RFC3339)
	}
	created := "—"
	if !t.CreatedAt.IsZero() {
		created = t.CreatedAt.Format(time.RFC3339)
	}
	b.WriteString(fmt.Sprintf("  Started   %s\n", started))
	b.WriteString(fmt.Sprintf("  Created   %s\n", created))

	if len(t.Containers) > 0 {
		b.WriteString("\n")
		b.WriteString(themeTitle().Render("Containers"))
		b.WriteString("\n\n")
		for _, c := range t.Containers {
			b.WriteString(themeDim().Render("    · "))
			b.WriteString(c)
			b.WriteString("\n")
		}
	}

	return b.String()
}

func formatDeploymentLine(d cloud.DeploymentInfo) string {
	var icon, state string
	switch d.RolloutState {
	case "COMPLETED":
		icon = themeSuccess().Render("●")
		state = themeSuccess().Render("DONE")
	case "IN_PROGRESS":
		icon = inProgressStyle.Render("●")
		state = inProgressStyle.Render("IN_PROG")
	case "FAILED":
		icon = themeWarning().Render("x")
		state = themeWarning().Render("FAILED")
	default:
		icon = themeDim().Render("○")
		state = themeDim().Render(d.RolloutState)
	}

	age := relativeTime(d.CreatedAt)
	counts := fmt.Sprintf("%d/%d", d.RunningCount, d.DesiredCount)

	return fmt.Sprintf("  %s %-7s %-16s %6s %s",
		icon, state, d.TaskDef, age, themeDim().Render(counts))
}

func relativeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func (m model) renderConfirm() string {
	var b strings.Builder
	b.WriteString("\n")
	envLabel := strings.ToUpper(m.environment)
	b.WriteString(themeWarning().Render(fmt.Sprintf("  !  %s ACCESS", envLabel)))
	b.WriteString("\n\n")
	if m.dynamoMode {
		b.WriteString(themeDim().Render("    DynamoDB: list only tables whose names contain "))
		b.WriteString(m.environment)
		b.WriteString("\n")
	} else {
		b.WriteString(fmt.Sprintf("    Cluster:  %s\n", m.cluster))
		b.WriteString(fmt.Sprintf("    Service:  %s\n", m.service))
	}
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s\n", m.confirmInput.View()))
	return b.String()
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

func taskLabels(tasks []cloud.TaskInfo) []string {
	labels := make([]string, len(tasks))
	for i, t := range tasks {
		age := ""
		if !t.CreatedAt.IsZero() {
			age = t.CreatedAt.Format("2006-01-02 15:04:05")
		}
		labels[i] = fmt.Sprintf("%s  %s  %s", t.ShortID, t.Status, age)
	}
	return labels
}

func visibleWindow(total, cursor, maxVisible int) (start, end int) {
	if total <= maxVisible {
		return 0, total
	}
	half := maxVisible / 2
	start = cursor - half
	if start < 0 {
		start = 0
	}
	end = start + maxVisible
	if end > total {
		end = total
		start = end - maxVisible
	}
	return start, end
}
