package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"perch/internal/nono"
	"perch/internal/worktree"
)

// NewRequest is what the new-session dialog asks the backend to create.
type NewRequest struct {
	Name     string
	Dir      string   // absolute; the repo when Worktree is set
	Extra    []string // more absolute folders the session may use
	Profile  string
	Worktree bool
	Branch   string
}

type field int

const (
	fName field = iota
	fDir
	fExtra
	fProfile
	fWorktree
	fBranch
)

type dialogAction int

const (
	actNone dialogAction = iota
	actCancel
	actSubmit
	actPickFolder // open the fzf folder picker
)

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

const maxName = 40

type dialog struct {
	name, dir, branch textinput.Model
	extra             textinput.Model
	extras            []string // folders already added
	pickExtra         bool     // the next picked folder is an extra one
	profiles          []nono.Profile
	profErr           string
	prof              int
	worktree          bool
	focus             field
	submitted         bool // show "required" errors too
	busy              bool
	createErr         string
	existing          map[string]bool
	home              string
	command           func(NewRequest) (dir, cmd string)
}

func newDialog(profiles []nono.Profile, profErr string, existing []string, defaultDir, home string, command func(NewRequest) (string, string)) *dialog {
	mk := func(placeholder string) textinput.Model {
		t := textinput.New()
		t.Placeholder = placeholder
		t.Prompt = ""
		t.CharLimit = 300
		t.Cursor.SetMode(cursor.CursorStatic)
		return t
	}
	d := &dialog{
		name: mk("fix-login"), dir: mk("~/git/project"), branch: mk("feat/my-change"), extra: mk("~/git/other (enter adds)"),
		profiles: profiles, profErr: profErr, existing: map[string]bool{}, home: home, command: command,
	}
	for _, n := range existing {
		d.existing[n] = true
	}
	for i, p := range profiles {
		if p.Name == nono.DefaultProfile {
			d.prof = i
		}
	}
	d.dir.ShowSuggestions = true
	d.extra.ShowSuggestions = true
	d.dir.SetValue(defaultDir)
	d.dir.CursorEnd()
	d.setFocus(fName) // no suggestions until the user types: tab must not append a surprise "/"
	return d
}

func (d *dialog) refreshSuggestions() {
	d.dir.SetSuggestions(Completions(d.dir.Value(), d.home))
	d.extra.SetSuggestions(Completions(d.extra.Value(), d.home))
}

func (d *dialog) fields() []field {
	f := []field{fName, fDir, fExtra, fProfile, fWorktree}
	if d.worktree {
		f = append(f, fBranch)
	}
	return f
}

func (d *dialog) setFocus(f field) {
	d.focus = f
	d.name.Blur()
	d.dir.Blur()
	d.extra.Blur()
	d.branch.Blur()
	switch f {
	case fName:
		d.name.Focus()
	case fDir:
		d.dir.Focus()
	case fExtra:
		d.extra.Focus()
	case fBranch:
		d.branch.Focus()
	}
}

func (d *dialog) step(delta int) {
	fs := d.fields()
	for i, f := range fs {
		if f == d.focus {
			d.setFocus(fs[(i+delta+len(fs))%len(fs)])
			return
		}
	}
}

func (d *dialog) expandedDir() string {
	p := expandHome(strings.TrimSpace(d.dir.Value()), d.home)
	if p == "" {
		return ""
	}
	return filepath.Clean(p) // drops the trailing "/" that completion adds
}

func (d *dialog) expandedExtra() string {
	p := expandHome(strings.TrimSpace(d.extra.Value()), d.home)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// extraProblem says why the text in the extra-folder field cannot be added.
// Empty text is fine: nothing is pending.
func (d *dialog) extraProblem() string {
	p := d.expandedExtra()
	switch {
	case p == "":
		return ""
	case !strings.HasPrefix(p, "/"):
		return "use an absolute path or start with ~"
	}
	if fi, err := os.Stat(p); err != nil {
		return "folder does not exist"
	} else if !fi.IsDir() {
		return "not a folder"
	}
	for _, e := range d.extras {
		if e == p {
			return "folder is already added"
		}
	}
	if p == d.expandedDir() {
		return "this is already the main folder"
	}
	return ""
}

// addExtra moves a valid folder into the list. It reports whether it did.
func (d *dialog) addExtra(p string) bool {
	if p == "" {
		return false
	}
	for _, e := range d.extras {
		if e == p {
			return false
		}
	}
	d.extras = append(d.extras, p)
	return true
}

// errors returns the problem of each field. "Required" is held back until the
// first submit so an untouched form is not red.
func (d *dialog) errors() map[field]string {
	e := map[field]string{}
	name := d.name.Value()
	switch {
	case name == "":
		if d.submitted {
			e[fName] = "name is required"
		}
	case len(name) > maxName:
		e[fName] = fmt.Sprintf("name is longer than %d characters", maxName)
	case !nameRe.MatchString(name):
		e[fName] = "use letters, digits, . _ - (start with a letter or digit)"
	case d.existing[name]:
		e[fName] = "a session named " + name + " already exists"
	}

	raw := strings.TrimSpace(d.dir.Value())
	dir := d.expandedDir()
	dirOK := false
	switch {
	case raw == "":
		if d.submitted {
			e[fDir] = "folder is required"
		}
	case !strings.HasPrefix(dir, "/"):
		e[fDir] = "use an absolute path or start with ~"
	default:
		if fi, err := os.Stat(dir); err != nil {
			e[fDir] = "folder does not exist"
		} else if !fi.IsDir() {
			e[fDir] = "not a folder"
		} else if d.coversNonoState(dir) {
			e[fDir] = "nono refuses this folder: it contains nono's own state (~/.local/state/nono)"
		} else {
			dirOK = true
		}
	}

	if msg := d.extraProblem(); msg != "" {
		e[fExtra] = msg
	}

	if len(d.profiles) == 0 {
		e[fProfile] = firstNonEmpty(d.profErr, "no nono profiles found")
	}
	if d.worktree {
		switch {
		case d.branch.Value() == "":
			if d.submitted {
				e[fBranch] = "branch name is required"
			}
		default:
			if err := worktree.ValidateBranch(d.branch.Value()); err != nil {
				e[fBranch] = err.Error()
			}
		}
		if dirOK && !worktree.IsRepo(dir) {
			e[fWorktree] = "folder is not a git repository"
		}
	}
	return e
}

// coversNonoState reports whether granting dir would include nono's state
// root, which nono refuses ("overlaps protected nono state root"). That is
// the home folder and its ancestors.
func (d *dialog) coversNonoState(dir string) bool {
	if d.home == "" {
		return false
	}
	state := filepath.Join(d.home, ".local", "state", "nono")
	dir = filepath.Clean(dir)
	return state == dir || strings.HasPrefix(state, dir+string(filepath.Separator)) || dir == "/"
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (d *dialog) profileName() string {
	if d.prof < len(d.profiles) {
		return d.profiles[d.prof].Name
	}
	return ""
}

func (d *dialog) request() NewRequest {
	return NewRequest{
		Name: d.name.Value(), Dir: d.expandedDir(), Extra: append([]string(nil), d.extras...), Profile: d.profileName(),
		Worktree: d.worktree, Branch: d.branch.Value(),
	}
}

// update handles one key. The returned action tells the model what to do next.
func (d *dialog) update(k tea.KeyMsg) dialogAction {
	if d.busy {
		return actNone
	}
	d.createErr = ""
	switch k.String() {
	case "esc", "ctrl+c":
		return actCancel
	case "enter":
		if d.focus == fExtra && strings.TrimSpace(d.extra.Value()) != "" {
			if msg := d.extraProblem(); msg == "" && d.addExtra(d.expandedExtra()) {
				d.extra.SetValue("")
				d.extra.SetSuggestions(nil)
			}
			return actNone // errors() shows why when it was refused
		}
		d.submitted = true
		errs := d.errors()
		if len(errs) == 0 {
			return actSubmit
		}
		for _, f := range d.fields() {
			if errs[f] != "" {
				d.setFocus(f)
				break
			}
		}
		return actNone
	case "ctrl+f":
		d.pickExtra = d.focus == fExtra
		if !d.pickExtra {
			d.setFocus(fDir)
		}
		return actPickFolder
	case "down":
		return d.stepAndMaybePick(1)
	case "up", "shift+tab":
		return d.stepAndMaybePick(-1)
	case "tab":
		// on the folder field tab first accepts the suggestion
		if d.focus == fDir && d.dir.CurrentSuggestion() != "" && d.dir.CurrentSuggestion() != d.dir.Value() {
			d.dir, _ = d.dir.Update(k)
			d.refreshSuggestions()
			return actNone
		}
		if d.focus == fExtra && d.extra.CurrentSuggestion() != "" && d.extra.CurrentSuggestion() != d.extra.Value() {
			d.extra, _ = d.extra.Update(k)
			d.refreshSuggestions()
			return actNone
		}
		return d.stepAndMaybePick(1)
	}

	switch d.focus {
	case fProfile:
		switch k.String() {
		case "left", "h":
			d.moveProfile(-1)
		case "right", "l":
			d.moveProfile(1)
		}
	case fWorktree:
		if k.String() == " " || k.String() == "x" {
			d.worktree = !d.worktree
		}
	case fName:
		d.name, _ = d.name.Update(k)
	case fDir:
		d.dir, _ = d.dir.Update(k)
		d.refreshSuggestions()
	case fExtra:
		if k.String() == "backspace" && d.extra.Value() == "" && len(d.extras) > 0 {
			d.extras = d.extras[:len(d.extras)-1] // backspace on empty text removes the last folder
			return actNone
		}
		d.extra, _ = d.extra.Update(k)
		d.refreshSuggestions()
	case fBranch:
		d.branch, _ = d.branch.Update(k)
	}
	return actNone
}

// stepAndMaybePick moves focus. Arriving on an empty folder field opens the
// picker, so the folder never has to be typed.
func (d *dialog) stepAndMaybePick(delta int) dialogAction {
	d.step(delta)
	if d.focus == fDir && strings.TrimSpace(d.dir.Value()) == "" {
		return actPickFolder
	}
	return actNone
}

func (d *dialog) setFolder(p string) {
	if d.pickExtra {
		d.pickExtra = false
		d.addExtra(filepath.Clean(p))
		return
	}
	d.dir.SetValue(p)
	d.dir.CursorEnd()
	d.dir.SetSuggestions(nil) // a picked folder is final: no ghost "/" completion
}

func (d *dialog) moveProfile(delta int) {
	if n := len(d.profiles); n > 0 {
		d.prof = (d.prof + delta + n) % n
	}
}

// previewRequest fills blanks with placeholders so the command preview is
// always readable, even before the form is valid.
func (d *dialog) previewRequest() NewRequest {
	r := d.request()
	if r.Name == "" {
		r.Name = "<name>"
	}
	if r.Dir == "" {
		r.Dir = "<folder>"
	}
	if r.Worktree && r.Branch == "" {
		r.Branch = "<branch>"
	}
	return r
}

const labelW = 14

var (
	labelSty = lipgloss.NewStyle().Width(labelW)
	focusSty = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

func (d *dialog) view(width int) string {
	errs := d.errors()
	w := width - 8
	if w > 88 {
		w = 88
	}
	if w < 30 {
		w = 30
	}
	inner := w - labelW - 6
	d.name.Width, d.dir.Width, d.branch.Width, d.extra.Width = inner, inner, inner, inner

	label := func(f field, s string) string {
		if d.focus == f {
			return focusSty.Render(labelSty.Render("▸ " + s))
		}
		return subSty.Render(labelSty.Render("  " + s))
	}
	errLine := func(f field) string {
		if errs[f] == "" {
			return ""
		}
		return "\n" + strings.Repeat(" ", labelW) + errSty.Render("✗ "+trunc(errs[f], w-labelW-2))
	}

	var b strings.Builder
	b.WriteString(bannerSty.Render(" New session ") + "\n\n")
	b.WriteString(label(fName, "Name") + d.name.View() + errLine(fName) + "\n")
	hint := ""
	if d.focus == fDir {
		hint = "\n" + strings.Repeat(" ", labelW) + subSty.Render("ctrl+f: pick with fzf")
	}
	b.WriteString(label(fDir, "Folder") + d.dir.View() + errLine(fDir) + hint + "\n")

	chips := ""
	for _, e := range d.extras {
		chips += strings.Repeat(" ", labelW) + subSty.Render(trunc("+ "+tildify(e, d.home), w-labelW-2)) + "\n"
	}
	exHint := ""
	if d.focus == fExtra {
		exHint = "\n" + strings.Repeat(" ", labelW) + subSty.Render("enter add · ctrl+f fzf · backspace on empty removes last")
	}
	b.WriteString(label(fExtra, "Also allow") + d.extra.View() + errLine(fExtra) + exHint + "\n" + chips)

	prof := dim.Render("(none)")
	desc := ""
	if p := d.profileName(); p != "" {
		prof = frameSty.Render("◂ ") + titleSty.Render(p) + frameSty.Render(" ▸")
		desc = d.profiles[d.prof].Description
	}
	b.WriteString(label(fProfile, "Profile") + prof + errLine(fProfile) + "\n")
	if desc != "" {
		b.WriteString(strings.Repeat(" ", labelW) + subSty.Render(trunc(desc, w-labelW-2)) + "\n")
	}

	box := subSty.Render("[ ]")
	if d.worktree {
		box = frameSty.Render("[x]")
	}
	b.WriteString(label(fWorktree, "Worktree") + box + subSty.Render(" new git worktree for a branch") + errLine(fWorktree) + "\n")
	if d.worktree {
		b.WriteString(label(fBranch, "Branch") + d.branch.View() + errLine(fBranch) + "\n")
	}

	dir, cmd := d.command(d.previewRequest())
	b.WriteString("\n" + subSty.Render("Runs in "+dir) + "\n")
	b.WriteString(lipgloss.NewStyle().Width(w-2).Render(descSty.Render(cmd)) + "\n")

	switch {
	case d.busy:
		b.WriteString("\n" + frameSty.Render("creating…"))
	case d.createErr != "":
		b.WriteString("\n" + errSty.Render(trunc("✗ "+d.createErr, w-2)))
	default:
		b.WriteString("\n" + keySty.Render("enter") + " " + descSty.Render("create") + sepSty.Render(" • ") + keySty.Render("esc") + " " + descSty.Render("cancel"))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(1, 2).Width(w).Render(b.String())
}
