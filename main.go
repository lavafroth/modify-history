package main

import (
	"bytes"
	"fmt"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"log"
	"os"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing/object"
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type Item struct {
	title, desc string
}

func (i Item) Title() string       { return i.title }
func (i Item) Description() string { return i.desc }
func (i Item) FilterValue() string { return i.title }

type Model struct {
	path         string
	list         list.Model
	chosen       *int
	originalZone *time.Location
	textInput    textinput.Model
	commits      []*object.Commit
}

func (m Model) Init() tea.Cmd {
	return nil
}

func newTextField() textinput.Model {
	t := textinput.New()
	t.Placeholder = "2025-08-31"
	t.Focus()
	t.CharLimit = 64
	t.Width = 40

	return t
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.chosen != nil {
		return m.TextUpdate(msg)
	}
	return m.ListUpdate(msg)
}

func (m Model) TextUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			m.ChangeDate()
			m.FetchGitLog()
			m.chosen = nil
		case tea.KeyEsc:
			m.chosen = nil
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) ListUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", " ":
			m.chosen = new(int)
			*m.chosen = m.list.Cursor()
			m.originalZone = m.commits[m.list.Cursor()].Committer.When.Location()
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) View() string {
	if m.chosen != nil {
		return fmt.Sprintf("Enter the new date in ISO 8601 yyyy-mm-dd format:\n\n%s\n\n(esc to quit)\n", m.textInput.View())
	}
	return docStyle.Render(m.list.View())
}

func reconstructTimeInZone(date time.Time, loc *time.Location) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), date.Hour(), date.Minute(), date.Second(), date.Nanosecond(), loc)
}

func (m Model) ChangeDate() {
	date := m.textInput.Value()
	parsedDate, err := time.Parse("2006-01-02", date)
	if err != nil {
		log.Fatalf("failed to parse new date: %v", err)
	}

	timedelta := rand.Int63n(int64(time.Hour * 24))
	parsedDate = parsedDate.Add(time.Duration(timedelta))
	parsedDate = reconstructTimeInZone(parsedDate, m.originalZone)
	dateString := parsedDate.String()
	rebaseRelativeToHead := m.list.Cursor() + 1
	rebaseHash := m.commits[m.list.Cursor()].Hash.String()
	rebase(rebaseRelativeToHead, rebaseHash)
	cmd := exec.Command("git", "commit", "--amend", "--no-edit", "--date", dateString)
	committerEnv := fmt.Sprintf("GIT_COMMITTER_DATE=%v", dateString)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, committerEnv)

	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to create amend commit with git: %v: %v", err, errb.String())
	}

	cmd = exec.Command("git", "rebase", "--continue")
	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to continue rebase git: %v", err)
	}

	for reflog := range m.list.Cursor() + 4 {
		cmd = exec.Command("git", "reflog", "delete", "HEAD@{0}")
		if err := cmd.Run(); err != nil {
			log.Fatalf("failed to delete reflog %d: %v", reflog+1, err)
		}
	}

}

func rebase(nthCommit int, rebaseHash string) {
	myPath, err := os.Executable()
	if err != nil {
		log.Fatalf("could not find a path to the running binary: %v", err)
	}
	// Avoid the default Windows backslash path separator at all cost
	myPath = strings.ReplaceAll(myPath, "\\", "/")
	myEditMode := fmt.Sprintf("%s edit %s", myPath, rebaseHash)
	cmd := exec.Command("git", "rebase", "-i", fmt.Sprintf("HEAD~%d", nthCommit))
	seqEditorEnvVar := fmt.Sprintf("GIT_SEQUENCE_EDITOR=%v", myEditMode)
	cmd.Env = append(cmd.Env, seqEditorEnvVar)
	var errb bytes.Buffer
	cmd.Stderr = &errb

	if err := cmd.Run(); err != nil {
		log.Fatalf("failed to run git rebase command: %v, %v is git installed?", err, errb.String())
	}
}

func (m *Model) FetchGitLog() {
	r, err := git.PlainOpen(m.path)
	if err != nil {
		log.Fatal(err)
	}

	headRef, err := r.Head()
	cIter, err := r.Log(&git.LogOptions{From: headRef.Hash()})
	if err != nil {
		log.Fatal(err)
	}

	m.commits = []*object.Commit{}

	err = cIter.ForEach(func(c *object.Commit) error {
		m.commits = append(m.commits, c)
		return nil
	})

	if err != nil {
		log.Fatal(err)
	}
	items := []list.Item{}
	for _, c := range m.commits {
		oneLineMessage := strings.Split(c.Message, "\n")[0]
		item := Item{title: oneLineMessage, desc: fmt.Sprintf("%s %s", c.Committer.When, c.Hash.String())}
		items = append(items, item)
	}
	m.list.SetItems(items)
	m.list.Title = "Choose a commit"
}

func renderUI(path string) {
	if err := os.Chdir(path); err != nil {
		log.Fatalf("failed to switch to git repo directory: %v", err)
	}
	m := Model{path: ".", textInput: newTextField(), chosen: nil, list: list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0), originalZone: nil}
	m.FetchGitLog()
	p := tea.NewProgram(m, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}

}

func editGitTodo(commitHash string, fileName string) {
	b, err := os.ReadFile(fileName)
	if err != nil {
		log.Fatal(err)
	}
	content := string(b)
	commitHash = commitHash[:7]
	edit := strings.ReplaceAll(content, fmt.Sprintf("pick %s", commitHash), fmt.Sprintf("edit %s", commitHash))
	err = os.WriteFile(fileName, []byte(edit), 0o644)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	command := os.Args[1]
	if command == "open" {
		renderUI(os.Args[2])
	}
	if command == "edit" {
		editGitTodo(os.Args[2], os.Args[3])
	}
}
