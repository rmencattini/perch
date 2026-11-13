// Package ui is the Bubble Tea screen: header, session list, preview, keys.
package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"perch/internal/balance"
	"perch/internal/nono"
	"perch/internal/session"
	"perch/internal/status"
	"perch/internal/usage"
)

const (
	refreshEvery = time.Second
	headerLines  = 2 // banner + blank line; the body starts right after
	listTop      = 2 // "Sessions" banner + blank line above the first row
	rowHeight    = 3 // title, subtitle, blank line
	footerLines  = 2 // message + key bar
	listMinWidth = 32
)

type (
	tickMsg    struct{}
	refreshMsg struct {
		sessions []session.Session
		selected string // name the preview belongs to
		preview  string
		diff     string
		usage    usage.Summary
		balance  balance.State
		hint     string
		err      error
	}
	profilesMsg struct {
		profiles []nono.Profile
		err      error
	}
	createDoneMsg struct {
		name string
		err  error
	}
	// doneMsg ends an action; text goes to the message line.
	doneMsg struct {
		text string
		err  error
	}
)

type Model struct {
	b Backend

	sessions    []session.Session
	cursor      int
	preview     string
	diff        string // git diff of the selected session's folder, read only while the Diff tab shows
	tab         int    // 0 preview, 1 diff; only used once the user pressed tab
	tabSet      bool
	showPreview bool
	confirmKill bool
	dlg         *dialog // new-session dialog, nil when closed
	want        string  // select this session on the next refresh (just created)
	usage       usage.Summary
	balance     balance.State
	home        string
	now         func() time.Time
	pick        func(candidates []string) tea.Cmd // folder picker, fzf by default
	hint        string                            // how to detach, from the user's tmux prefix
	msg         string
	msgErr      bool
	width       int
	height      int
}

func New(b Backend) Model {
	home, _ := os.UserHomeDir()
	return Model{b: b, showPreview: true, width: 100, height: 30, home: home, now: time.Now, pick: fzfPick}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) selected() (session.Session, bool) {
	if m.cursor < 0 || m.cursor >= len(m.sessions) {
		return session.Session{}, false
	}
	return m.sessions[m.cursor], true
}

// refresh re-reads sessions and the preview of the selected one, off the UI loop.
func (m Model) refresh() tea.Cmd {
	sel, _ := m.selected()
	if m.want != "" {
		sel.Name = m.want
	}
	b, wantDiff := m.b, m.diffTab
	fill := func(out *refreshMsg, s session.Session) {
		out.selected, out.preview = s.Name, b.Preview(s)
		if wantDiff(s) {
			out.diff = b.Diff(s)
		}
	}
	return func() tea.Msg {
		ss, err := b.List()
		if err != nil {
			return refreshMsg{err: err}
		}
		out := refreshMsg{sessions: ss, usage: b.Usage(), balance: b.Balance(), hint: b.DetachHint()}
		for _, s := range ss {
			if s.Name == sel.Name {
				fill(&out, s)
				return out
			}
		}
		if len(ss) > 0 {
			fill(&out, ss[0])
		}
		return out
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())
	case refreshMsg:
		if msg.err != nil {
			m.msg, m.msgErr = msg.err.Error(), true
			return m, nil
		}
		// The cursor belongs to the user, not to the refresh: a refresh that
		// started before a key press must not move it back.
		keep, _ := m.selected()
		if m.want != "" {
			keep.Name = m.want
		}
		if keep.Name == "" {
			keep.Name = msg.selected // first load: nothing was selected yet
		}
		m.sessions = msg.sessions
		m.cursor = indexOf(m.sessions, keep.Name, m.cursor)
		m.usage, m.balance = msg.usage, msg.balance
		m.hint = msg.hint
		if cur, ok := m.selected(); ok && cur.Name == m.want {
			m.want = ""
		}
		if msg.selected == keep.Name {
			m.preview, m.diff = msg.preview, msg.diff
		} else {
			return m, m.refresh() // preview is for a row the user left: fetch the right one
		}
	case profilesMsg:
		return m.openDialog(msg), nil
	case pickDoneMsg:
		if m.dlg != nil {
			switch {
			case msg.err != nil:
				m.dlg.createErr = msg.err.Error()
			case msg.path != "":
				m.dlg.setFolder(msg.path)
			default:
				m.dlg.pickExtra = false // esc in fzf
			}
		}
		return m, nil
	case createDoneMsg:
		if msg.err != nil {
			if m.dlg != nil {
				m.dlg.busy = false
				m.dlg.createErr = msg.err.Error()
			}
			return m, nil
		}
		m.dlg = nil
		m.want = msg.name
		m.msg, m.msgErr = "created "+msg.name+" (enter to attach)", false
		return m, m.refresh()
	case doneMsg:
		m.msg, m.msgErr = msg.text, msg.err != nil
		if msg.err != nil {
			m.msg = msg.err.Error()
		}
		return m, m.refresh()
	case tea.KeyMsg:
		return m.onKey(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	}
	return m, nil
}

func indexOf(ss []session.Session, name string, fallback int) int {
	for i, s := range ss {
		if s.Name == name {
			return i
		}
	}
	if fallback >= len(ss) {
		fallback = len(ss) - 1
	}
	if fallback < 0 {
		fallback = 0
	}
	return fallback
}

func (m Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.dlg != nil {
		return m.onDialogKey(k)
	}
	if m.confirmKill {
		switch k.String() {
		case "y", "enter":
			m.confirmKill = false
			s, ok := m.selected()
			if !ok {
				return m, nil
			}
			return m, m.act(fmt.Sprintf("killed %s", s.Name), func() error { return m.b.Kill(s) })
		case "n", "esc", "q":
			m.confirmKill = false
			m.msg, m.msgErr = "kill cancelled", false
		}
		return m, nil
	}

	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		return m.move(1)
	case "k", "up":
		return m.move(-1)
	case "b":
		m.showPreview = !m.showPreview
	case "tab":
		cur, _ := m.selected()
		showingDiff := m.diffTab(cur) // ask before tabSet changes the answer
		m.tabSet = true
		m.tab = 1
		if showingDiff {
			m.tab = 0
		}
		return m, m.refresh()
	case "n":
		b := m.b
		return m, func() tea.Msg {
			ps, err := b.Profiles()
			return profilesMsg{ps, err}
		}
	case "enter":
		if s, ok := m.selected(); ok {
			return m, m.attach(s)
		}
	case "K":
		if _, ok := m.selected(); ok {
			m.confirmKill = true
			m.msg = ""
		}
	}
	return m, nil
}

func (m Model) openDialog(p profilesMsg) Model {
	names := make([]string, len(m.sessions))
	for i, s := range m.sessions {
		names[i] = s.Name
	}
	errText := ""
	if p.err != nil {
		errText = "nono profile list failed: " + p.err.Error()
	}
	m.dlg = newDialog(p.profiles, errText, names, m.b.DefaultDir(), m.home, m.b.Command)
	m.msg = ""
	return m
}

func (m Model) onDialogKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.dlg.update(k) {
	case actCancel:
		m.dlg = nil
	case actPickFolder:
		return m, m.pick(Candidates(m.b.Folders(), RepoFolders(filepath.Join(m.home, "git")), m.home))
	case actSubmit:
		m.dlg.busy = true
		req, b := m.dlg.request(), m.b
		return m, func() tea.Msg { return createDoneMsg{name: req.Name, err: b.Create(req)} }
	}
	return m, nil
}

func (m Model) move(d int) (tea.Model, tea.Cmd) {
	n := m.cursor + d
	if n < 0 || n >= len(m.sessions) {
		return m, nil
	}
	m.cursor = n
	return m, m.refresh()
}

func (m Model) act(ok string, f func() error) tea.Cmd {
	return func() tea.Msg {
		if err := f(); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: ok}
	}
}

// attach hands the terminal to tmux until the user detaches (ctrl-b d).
func (m Model) attach(s session.Session) tea.Cmd {
	cmd, err := m.b.Attach(s)
	if err != nil {
		return func() tea.Msg { return doneMsg{err: err} }
	}
	var stderr bytes.Buffer
	if cmd.Stderr == nil {
		cmd.Stderr = &stderr // tmux says why it failed here; "exit status 1" alone helps nobody
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			if text := strings.TrimSpace(stderr.String()); text != "" {
				err = fmt.Errorf("attach failed: %s", strings.ReplaceAll(text, "\n", " · "))
			} else {
				err = fmt.Errorf("attach failed: %w", err)
			}
			return doneMsg{err: err}
		}
		return doneMsg{text: "detached from " + s.Name}
	})
}

func (m Model) listWidth() int {
	if !m.showPreview {
		return m.width
	}
	w := m.rowWidth()
	if w < listMinWidth {
		w = listMinWidth
	}
	if max := m.width * 3 / 5; w > max {
		w = max
	}
	return w
}

// nameWidth is the longest session name, so the list fits its content.
func (m Model) nameWidth() int {
	n := 0
	for _, s := range m.sessions {
		if w := lipgloss.Width(s.Name); w > n {
			n = w
		}
	}
	return n
}

// rowWidth is the width of the widest row: mark, symbol, name, status.
func (m Model) rowWidth() int {
	st := 0
	for _, s := range m.sessions {
		if w := lipgloss.Width(s.Status.String()); w > st {
			st = w
		}
	}
	return 5 + 2 + m.nameWidth() + 2 + st // " 1. " + symbol + name + gap + status
}

func (m Model) onMouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.confirmKill || m.dlg != nil || e.Action != tea.MouseActionPress || e.Button != tea.MouseButtonLeft {
		return m, nil
	}
	row := (e.Y - headerLines - listTop) / rowHeight
	if e.Y-headerLines-listTop < 0 || e.X >= m.listWidth() || row >= len(m.sessions) {
		return m, nil
	}
	m.cursor = row
	return m, m.refresh()
}

var (
	bold   = lipgloss.NewStyle().Bold(true)
	dim    = lipgloss.NewStyle().Faint(true)
	errSty = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))

	// Palette follows Claude Squad: slate banner, purple frame, soft grey text.
	accent    = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	bannerSty = lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230"))
	frameSty  = lipgloss.NewStyle().Foreground(accent)
	titleSty  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})
	subSty    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})
	selSty    = lipgloss.NewStyle().Background(lipgloss.Color("#dde4f0")).Foreground(lipgloss.Color("#1a1a1a"))
	keySty    = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
	descSty   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7A7474", Dark: "#9C9494"})
	sepSty    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#DDDADA", Dark: "#3C3C3C"})
	stSty     = map[status.Status]lipgloss.Style{
		status.Detached:   lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		status.Elsewhere:  lipgloss.NewStyle().Foreground(lipgloss.Color("6")),
		status.Running:    lipgloss.NewStyle().Foreground(lipgloss.Color("#51bd73")),
		status.NeedsInput: lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true),
		status.Idle:       lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
	}
)

func (m Model) View() string {
	bodyH := m.height - headerLines - footerLines
	if bodyH < 1 {
		bodyH = 1
	}
	var body string
	if m.dlg != nil {
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, m.dlg.view(m.width))
	} else if m.confirmKill {
		body = m.killModal(bodyH)
	} else {
		body = m.listAndPreview(bodyH)
	}
	return strings.Join([]string{m.header(), "", body, m.messageLine(), m.keyBar()}, "\n")
}

func (m Model) header() string {
	waiting := 0
	for _, s := range m.sessions {
		if s.Status == status.NeedsInput {
			waiting++
		}
	}
	h := bannerSty.Render(" perch ") + dim.Render(fmt.Sprintf("  %d sessions", len(m.sessions)))
	if waiting > 0 {
		h += "  " + stSty[status.NeedsInput].Render(fmt.Sprintf("! %d need input", waiting))
	}
	u := m.usageBars()
	if d := m.balanceText(); d != "" {
		u = d + "  " + u
	}
	if gap := m.width - lipgloss.Width(h) - lipgloss.Width(u); gap >= 2 {
		h += strings.Repeat(" ", gap) + u
	}
	return h
}

// balanceText is the DeepSeek credit for the header. No key found says so, so a wrong
// Keychain name is not silent; loading and failures stay short and never quote the key.
func (m Model) balanceText() string {
	b := m.balance
	switch {
	case !b.Configured:
		return ""
	case b.Missing:
		return dim.Render("DeepSeek: no key")
	case !b.Loaded:
		return dim.Render("DeepSeek …")
	case errors.Is(b.Err, balance.ErrExhausted) || (b.Err == nil && !b.Info.Available):
		return errSty.Render("DeepSeek empty ✗")
	case b.Err != nil:
		return dim.Render("DeepSeek ?")
	}
	return "DeepSeek " + b.Info.Total + " " + b.Info.Currency
}

const barCells = 8

func (m Model) usageBars() string {
	return bar("5h", m.usage.FiveHour) + "  " + bar("7d", m.usage.SevenDay)
}

// bar draws one window. Missing or stale data is "unknown" with an empty gray
// bar, never 0%.
func bar(label string, r usage.Reading) string {
	if !r.Known {
		return label + " " + dim.Render(strings.Repeat("░", barCells)+" unknown")
	}
	pct := r.Percent
	filled := int(pct/100*barCells + 0.5)
	if filled > barCells {
		filled = barCells
	}
	if filled < 0 {
		filled = 0
	}
	col := "2"
	switch {
	case pct >= 90:
		col = "1"
	case pct >= 70:
		col = "3"
	}
	cells := lipgloss.NewStyle().Foreground(lipgloss.Color(col)).Render(strings.Repeat("█", filled)) +
		dim.Render(strings.Repeat("░", barCells-filled))
	out := fmt.Sprintf("%s %s %.0f%%", label, cells, pct)
	if r.Age >= 2*time.Minute {
		out += dim.Render(fmt.Sprintf(" %dm old", int(r.Age.Minutes())))
	}
	return out
}

func (m Model) listAndPreview(h int) string {
	lw := m.listWidth()
	rows := []string{" " + bannerSty.Render(" Sessions "), ""}
	if len(m.sessions) == 0 {
		rows = append(rows, " "+dim.Render("no sessions yet"))
	}
	for i, s := range m.sessions {
		rows = append(rows, m.row(i, s, lw), "")
	}
	list := lipgloss.NewStyle().Width(lw).MaxWidth(lw).Height(h).MaxHeight(h).Render(strings.Join(rows, "\n"))
	if !m.showPreview {
		return list
	}
	pw := m.width - lw - 1
	if pw < 1 {
		return list
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, list, " ", m.previewPane(pw, h))
}

// row is two lines: " 1. ● name   status" and the folder and profile under it.
// The selected row gets a light bar across the full list width.
func (m Model) row(i int, s session.Session, w int) string {
	nw := m.nameWidth()
	st := s.Status.String()
	head := fmt.Sprintf(" %d. %s %-*s", i+1, s.Status.Symbol(), nw, trunc(s.Name, nw))
	gap := w - lipgloss.Width(head) - lipgloss.Width(st) - 1
	if gap < 1 {
		gap = 1
	}
	head = trunc(head+strings.Repeat(" ", gap)+st, w)
	where := strings.TrimSpace(s.Dir)
	if where == "" {
		where = s.NonoID
	}
	if s.Profile != "" {
		where += " · " + s.Profile
	}
	sub := trunc("    "+strings.TrimSpace(where), w)
	line := lipgloss.NewStyle().Width(w).MaxWidth(w)
	if i == m.cursor {
		return selSty.Inherit(line).Render(head) + "\n" + selSty.Inherit(line).Render(sub)
	}
	// the symbol keeps its status color, the rest stays quiet
	sym := s.Status.Symbol()
	if k := strings.Index(head, sym); k >= 0 {
		head = titleSty.Render(head[:k]) + stSty[s.Status].Render(sym) + titleSty.Render(head[k+len(sym):])
	} else {
		head = titleSty.Render(head)
	}
	return line.Render(head) + "\n" + line.Render(subSty.Render(sub))
}

func (m Model) previewPane(w, h int) string {
	tabs := []string{" Preview ", " Diff "}
	minW := 2
	for _, t := range tabs {
		minW += lipgloss.Width(t) + 2
	}
	s, ok := m.selected()
	if w < minW || h < 5 {
		return lipgloss.NewStyle().Width(w).MaxWidth(w).Height(h).MaxHeight(h).Render("")
	}
	showDiff := ok && m.diffTab(s)
	active := 0
	if showDiff {
		active = 1
	}

	cw := w - 4 // frame and one space of padding each side
	rows := h - 4
	var inner []string
	if !ok {
		inner = []string{dim.Render("nothing selected")}
	} else {
		inner = append(inner,
			stSty[s.Status].Bold(true).Render(trunc(s.Status.Symbol()+" "+s.Name, cw)),
			subSty.Render(trunc(strings.TrimSpace(s.Dir+"  "+s.Profile), cw)))
		text, tail := m.preview, true // newest pane lines stay in view
		if showDiff {
			text, tail = m.diff, false // a diff reads from the top
			if strings.TrimSpace(text) == "" {
				text = "no changes in " + s.Dir
			}
		}
		lines := strings.Split(strings.TrimRight(text, "\n "), "\n")
		cut := lipgloss.NewStyle().MaxWidth(cw)
		for i, l := range lines {
			lines[i] = colorLine(l, cut.Render(l))
		}
		avail := rows - 2
		switch {
		case avail < 1:
			lines = nil
		case len(lines) > avail && tail:
			lines = lines[len(lines)-avail:]
		case len(lines) > avail:
			lines = lines[:avail]
		}
		inner = append(inner, lines...)
	}

	// Tab row: each tab is a small rounded box; the active one opens into the
	// frame below, the others close with ┴.
	f := frameSty.Render
	var top, mid []string
	box := []rune("╭" + strings.Repeat("─", w-2) + "╮")
	x := 0
	for i, t := range tabs {
		tw := lipgloss.Width(t) + 2
		l, r := x, x+tw-1
		edge, label := dim.Render, dim.Render(t)
		if i == active {
			edge, label = f, bold.Render(t)
			box[l], box[r] = '┘', '└'
			if l == 0 {
				box[l] = '│'
			}
			for c := l + 1; c < r; c++ {
				box[c] = ' '
			}
		} else {
			box[l], box[r] = '┴', '┴'
			if l == 0 {
				box[l] = '├'
			}
		}
		top = append(top, edge("╭"+strings.Repeat("─", tw-2)+"╮"))
		mid = append(mid, edge("│")+label+edge("│"))
		x += tw
	}
	rest := strings.Repeat(" ", w-x)
	out := []string{strings.Join(top, "") + rest, strings.Join(mid, "") + rest, f(string(box))}
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(inner) {
			line = inner[i]
		}
		out = append(out, f("│")+" "+line+strings.Repeat(" ", cw-lipgloss.Width(line))+" "+f("│"))
	}
	out = append(out, f("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

// diffTab says whether the panel shows the diff. Preview is the default; tab
// switches. The status is no clue: it can be wrong, so it never picks the tab.
func (m Model) diffTab(session.Session) bool {
	return m.tabSet && m.tab == 1
}

var (
	addSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	delSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	promptSty = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	agentSty  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	warnSty   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

var errWord = regexp.MustCompile(`(?i)\b(error|failed|fatal|panic)\b`)

// colorLine tints one preview line by what it looks like. plain decides the
// style, cut is the same text already trimmed to the pane width.
func colorLine(plain, cut string) string {
	t := strings.TrimSpace(plain)
	switch {
	case t == "":
		return cut
	case strings.HasPrefix(t, "❯"), strings.HasPrefix(t, "> "):
		return promptSty.Render(cut)
	case strings.HasPrefix(t, "⏺"), strings.HasPrefix(t, "●"):
		return agentSty.Render(cut)
	case strings.HasPrefix(t, "✗"), strings.HasPrefix(t, "✘"), errWord.MatchString(t):
		return delSty.Render(cut)
	case strings.HasPrefix(t, "✓"), strings.HasPrefix(t, "✔"):
		return addSty.Render(cut)
	case strings.HasPrefix(plain, "+") && !strings.HasPrefix(plain, "+++"):
		return addSty.Render(cut)
	case strings.HasPrefix(plain, "-") && !strings.HasPrefix(plain, "---"):
		return delSty.Render(cut)
	case strings.HasPrefix(t, "⎿"), strings.HasPrefix(t, "│"), strings.HasPrefix(t, "─"), strings.HasPrefix(t, "╭"), strings.HasPrefix(t, "╰"):
		return dim.Render(cut)
	case strings.Contains(t, "(y/n)"), strings.Contains(t, "[Y/n]"), strings.HasPrefix(t, "Do you want"):
		return warnSty.Render(cut)
	}
	return cut
}

func (m Model) killModal(h int) string {
	s, _ := m.selected()
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 3).Render(
		bold.Render("Kill "+s.Name+"?") + "\n\n" +
			"This stops the session for real (nono stop).\n" +
			"Unsaved work in it is lost.\n\n" +
			"y confirm · n / esc cancel")
	return lipgloss.Place(m.width, h, lipgloss.Center, lipgloss.Center, box)
}

func (m Model) messageLine() string {
	if m.msgErr {
		return errSty.Render(trunc(m.msg, m.width))
	}
	return dim.Render(trunc(m.msg, m.width))
}

type keyHint struct{ key, desc string }

// hintBar renders groups of key hints, centered: "k desc • k desc │ k desc".
func (m Model) hintBar(groups ...[]keyHint) string {
	var gs []string
	for _, g := range groups {
		var ps []string
		for _, h := range g {
			ps = append(ps, keySty.Render(h.key)+" "+descSty.Render(h.desc))
		}
		gs = append(gs, strings.Join(ps, sepSty.Render(" • ")))
	}
	bar := lipgloss.NewStyle().MaxWidth(m.width).Render(strings.Join(gs, sepSty.Render(" │ ")))
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, bar)
}

func (m Model) keyBar() string {
	if m.confirmKill {
		return m.hintBar([]keyHint{{"y", "confirm"}, {"n/esc", "cancel"}})
	}
	if m.dlg != nil {
		return m.hintBar(
			[]keyHint{{"tab/↑↓", "field"}, {"←→", "profile"}, {"space", "worktree"}},
			[]keyHint{{"ctrl+f", "folder (fzf)"}, {"enter", "create"}, {"esc", "cancel"}})
	}
	enter := "attach"
	if m.hint != "" {
		enter += " (" + m.hint + " back)"
	}
	return m.hintBar(
		[]keyHint{{"n", "new"}, {"K", "kill"}},
		[]keyHint{{"enter", enter}},
		[]keyHint{{"tab", "diff"}, {"b", "panel"}, {"q", "quit"}})
}

func trunc(s string, w int) string {
	if w < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}
