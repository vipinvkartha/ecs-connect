package tui

import (
	"fmt"
	"strings"
	"time"

	"ecs-connect/internal/cloud"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39"))

	selectedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("242"))

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("82"))

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)

	previewBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(1, 2).
			Width(52)

	breadcrumbStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("242")).
			Italic(true)
)

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m model) View() string {
	var b strings.Builder

	switch m.step {
	case stepCheckAuth:
		b.WriteString(m.breadcrumb())
		b.WriteString(fmt.Sprintf("\n  %s Checking AWS credentials...\n", m.spinner.View()))

	case stepSelectEnv:
		b.WriteString(m.breadcrumb())
		b.WriteString(m.renderList("Select Environment", m.applyFilter(m.envItems), m.envCursor))

	case stepLoadClusters, stepLoadServices, stepLoadTasks:
		b.WriteString(m.breadcrumb())
		b.WriteString(fmt.Sprintf("\n  %s %s\n", m.spinner.View(), m.loadingMsg))

	case stepSelectCluster:
		b.WriteString(m.breadcrumb())
		b.WriteString(m.renderList("Select Cluster", m.applyFilter(m.clusterItems), m.clusterCursor))

	case stepSelectService:
		b.WriteString(m.breadcrumb())
		visible := m.applyFilter(m.serviceItems)
		list := m.renderList("Select Service", visible, m.serviceCursor)
		preview := m.renderPreview()
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", preview))

	case stepConfirm:
		b.WriteString(m.breadcrumb())
		b.WriteString(m.renderConfirm())

	case stepSelectTask:
		b.WriteString(m.breadcrumb())
		b.WriteString(m.renderList("Select Task", m.applyFilter(taskLabels(m.taskItems)), m.taskCursor))

	case stepSelectContainer:
		b.WriteString(m.breadcrumb())
		b.WriteString(m.renderList("Select Container", m.applyFilter(m.containerItems), m.containerCursor))
	}

	if m.filterActive {
		b.WriteString(dimStyle.Render("\n  ↑/↓ navigate • enter select • esc clear filter\n"))
	} else {
		b.WriteString(dimStyle.Render("\n  ↑/↓ navigate • enter select • / filter • esc cancel\n"))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Rendering helpers
// ---------------------------------------------------------------------------

func (m model) breadcrumb() string {
	var parts []string
	if m.profile != "" {
		parts = append(parts, m.profile)
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
	if len(parts) == 0 {
		return "\n"
	}
	return "\n" + breadcrumbStyle.Render("  "+strings.Join(parts, " → ")) + "\n"
}

func (m model) renderList(title string, items []string, cursor int) string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("  " + title))
	b.WriteString("\n")

	if m.filterActive && m.filterText != "" {
		b.WriteString(dimStyle.Render("  filter: ") + m.filterText)
		b.WriteString("\n")
	}

	b.WriteString("\n")

	if len(items) == 0 {
		b.WriteString(dimStyle.Render("    (no matches)"))
		b.WriteString("\n")
		return b.String()
	}

	maxVisible := m.height - 10
	if maxVisible < 5 {
		maxVisible = 5
	}
	start, end := visibleWindow(len(items), cursor, maxVisible)

	if start > 0 {
		b.WriteString(dimStyle.Render("    ↑ more"))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		if i == cursor {
			b.WriteString(selectedStyle.Render("  ▸ " + items[i]))
		} else {
			b.WriteString(normalStyle.Render("    " + items[i]))
		}
		b.WriteString("\n")
	}
	if end < len(items) {
		b.WriteString(dimStyle.Render("    ↓ more"))
		b.WriteString("\n")
	}
	return b.String()
}

func (m model) renderPreview() string {
	var content string
	if m.previewLoading {
		content = fmt.Sprintf("%s Loading...", m.spinner.View())
	} else if m.currentPreview != nil {
		content = formatServiceInfo(m.currentPreview)
	} else {
		content = dimStyle.Render("No preview")
	}
	return previewBorder.Render(content)
}

func formatServiceInfo(info *cloud.ServiceInfo) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Service Details"))
	b.WriteString("\n\n")

	status := info.Status
	if status == "ACTIVE" {
		status = successStyle.Render(status)
	} else {
		status = warningStyle.Render(status)
	}
	b.WriteString(fmt.Sprintf("  Status    %s\n", status))
	b.WriteString(fmt.Sprintf("  Desired   %d\n", info.DesiredCount))

	runStr := fmt.Sprintf("%d", info.RunningCount)
	if info.RunningCount == info.DesiredCount {
		runStr = successStyle.Render(runStr)
	} else {
		runStr = warningStyle.Render(runStr)
	}
	b.WriteString(fmt.Sprintf("  Running   %s\n", runStr))
	b.WriteString(fmt.Sprintf("  Pending   %d\n", info.PendingCount))
	b.WriteString(fmt.Sprintf("  TaskDef   %s\n", info.TaskDef))

	if len(info.Deployments) > 0 {
		b.WriteString("\n")
		b.WriteString(titleStyle.Render("Recent Deployments"))
		b.WriteString("\n\n")
		for _, d := range info.Deployments {
			b.WriteString(formatDeploymentLine(d))
			b.WriteString("\n")
		}
	}

	return b.String()
}

var (
	inProgressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("220"))
)

func formatDeploymentLine(d cloud.DeploymentInfo) string {
	var icon, state string
	switch d.RolloutState {
	case "COMPLETED":
		icon = successStyle.Render("●")
		state = successStyle.Render("DONE")
	case "IN_PROGRESS":
		icon = inProgressStyle.Render("●")
		state = inProgressStyle.Render("IN_PROG")
	case "FAILED":
		icon = warningStyle.Render("✗")
		state = warningStyle.Render("FAILED")
	default:
		icon = dimStyle.Render("○")
		state = dimStyle.Render(d.RolloutState)
	}

	age := relativeTime(d.CreatedAt)
	counts := fmt.Sprintf("%d/%d", d.RunningCount, d.DesiredCount)

	return fmt.Sprintf("  %s %-7s %-16s %6s %s",
		icon, state, d.TaskDef, age, dimStyle.Render(counts))
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
	b.WriteString(warningStyle.Render(fmt.Sprintf("  ⚠  %s ACCESS", envLabel)))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("    Cluster:  %s\n", m.cluster))
	b.WriteString(fmt.Sprintf("    Service:  %s\n", m.service))
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
