package tree

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/lexers"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/soft-serve/internal/tui/bubbles/git/types"
	vp "github.com/charmbracelet/soft-serve/internal/tui/bubbles/git/viewport"
	"github.com/charmbracelet/soft-serve/internal/tui/style"
	"github.com/dustin/go-humanize"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type fileMsg struct {
	content string
}

type sessionState int

const (
	treeState sessionState = iota
	fileState
	errorState
)

type item struct {
	*object.TreeEntry
	*object.File
}

func (i item) Name() string {
	return i.TreeEntry.Name
}

func (i item) Mode() filemode.FileMode {
	return i.TreeEntry.Mode
}

func (i item) FilterValue() string { return i.Name() }

type items []item

func (cl items) Len() int      { return len(cl) }
func (cl items) Swap(i, j int) { cl[i], cl[j] = cl[j], cl[i] }
func (cl items) Less(i, j int) bool {
	if cl[i].Mode() == filemode.Dir && cl[j].Mode() == filemode.Dir {
		return cl[i].Name() < cl[j].Name()
	} else if cl[i].Mode() == filemode.Dir {
		return true
	} else if cl[j].Mode() == filemode.Dir {
		return false
	} else {
		return cl[i].Name() < cl[j].Name()
	}
}

type itemDelegate struct {
	style *style.Styles
}

func (d itemDelegate) Height() int                               { return 1 }
func (d itemDelegate) Spacing() int                              { return 0 }
func (d itemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	s := d.style
	i, ok := listItem.(item)
	if !ok {
		return
	}

	name := i.Name()
	if i.Mode() == filemode.Dir {
		name = s.TreeFileDir.Render(name)
	}
	size := ""
	if i.File != nil {
		size = humanize.Bytes(uint64(i.File.Size))
	}
	var cs lipgloss.Style
	mode, _ := i.Mode().ToOSFileMode()
	if index == m.Index() {
		cs = s.TreeItemActive
		fmt.Fprint(w, s.TreeItemSelector.Render(">"))
	} else {
		cs = s.TreeItemInactive
		fmt.Fprint(w, s.TreeItemSelector.Render(" "))
	}
	leftMargin := s.TreeItemSelector.GetMarginLeft() +
		s.TreeItemSelector.GetWidth() +
		s.TreeFileMode.GetMarginLeft() +
		s.TreeFileMode.GetWidth() +
		cs.GetMarginLeft()
	rightMargin := s.TreeFileSize.GetMarginLeft() + lipgloss.Width(size)
	name = types.TruncateString(name, m.Width()-leftMargin-rightMargin, "…")
	sizeStyle := s.TreeFileSize.Copy().
		Width(m.Width() -
			leftMargin -
			s.TreeFileSize.GetMarginLeft() -
			lipgloss.Width(name)).
		Align(lipgloss.Right)
	fmt.Fprint(w, s.TreeFileMode.Render(mode.String())+
		cs.Render(name)+
		sizeStyle.Render(size))
}

type Bubble struct {
	repo         types.Repo
	list         list.Model
	style        *style.Styles
	width        int
	widthMargin  int
	height       int
	heightMargin int
	path         string
	state        sessionState
	error        types.ErrMsg
	fileViewport *vp.ViewportBubble
	lastSelected []int
}

func NewBubble(repo types.Repo, style *style.Styles, width, widthMargin, height, heightMargin int) *Bubble {
	l := list.New([]list.Item{}, itemDelegate{style}, width-widthMargin, height-heightMargin)
	l.SetShowFilter(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	l.KeyMap.NextPage = types.NextPage
	l.KeyMap.PrevPage = types.PrevPage
	b := &Bubble{
		fileViewport: &vp.ViewportBubble{
			Viewport: &viewport.Model{},
		},
		repo:         repo,
		style:        style,
		width:        width,
		height:       height,
		widthMargin:  widthMargin,
		heightMargin: heightMargin,
		list:         l,
		path:         "",
		state:        treeState,
		lastSelected: []int{},
	}
	b.SetSize(width, height)
	return b
}

func (b *Bubble) Init() tea.Cmd {
	b.path = ""
	b.list.Select(0)
	b.state = treeState
	return b.updateItems()
}

func (b *Bubble) SetSize(width, height int) {
	b.width = width
	b.height = height
	b.fileViewport.Viewport.Width = width - b.widthMargin
	b.fileViewport.Viewport.Height = height - b.heightMargin
	b.list.SetSize(width-b.widthMargin, height-b.heightMargin)
}

func (b *Bubble) Help() []types.HelpEntry {
	return nil
}

func (b *Bubble) updateItems() tea.Cmd {
	its := make(items, 0)
	t, err := b.repo.Tree(b.path)
	if err != nil {
		return func() tea.Msg { return types.ErrMsg{err} }
	}
	tw := object.NewTreeWalker(t, false, map[plumbing.Hash]bool{})
	defer tw.Close()
	for {
		_, e, err := tw.Next()
		if err != nil {
			break
		}
		i := item{
			TreeEntry: &e,
		}
		if e.Mode.IsFile() {
			if f, err := t.TreeEntryFile(&e); err == nil {
				i.File = f
			}
		}
		its = append(its, i)
	}
	sort.Sort(its)
	itt := make([]list.Item, len(its))
	for i, it := range its {
		itt[i] = it
	}
	cmd := b.list.SetItems(itt)
	b.list.Select(0)
	return cmd
}

func (b *Bubble) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0)
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		switch msg.String() {
		case "T":
			b.state = treeState
			b.path = ""
			cmds = append(cmds, b.updateItems())
		case "enter", "right", "l":
			if b.state == treeState {
				index := b.list.Index()
				item := b.list.SelectedItem().(item)
				mode := item.Mode()
				b.path = filepath.Join(b.path, item.Name())
				if mode == filemode.Dir {
					b.lastSelected = append(b.lastSelected, index)
					cmds = append(cmds, b.updateItems())
				} else {
					b.lastSelected = append(b.lastSelected, index)
					cmds = append(cmds, b.loadFile())
				}
			}
		case "esc", "left", "h":
			if b.state != treeState {
				b.state = treeState
			}
			p := filepath.Dir(b.path)
			b.path = p
			cmds = append(cmds, b.updateItems())
			index := 0
			if len(b.lastSelected) > 0 {
				index = b.lastSelected[len(b.lastSelected)-1]
				b.lastSelected = b.lastSelected[:len(b.lastSelected)-1]
			}
			b.list.Select(index)
		}

	case types.ErrMsg:
		b.error = msg
		b.state = errorState
		return b, nil

	case fileMsg:
		content := b.renderFile(msg)
		b.fileViewport.Viewport.SetContent(content)
		b.fileViewport.Viewport.GotoTop()
		b.state = fileState
	}

	switch b.state {
	case fileState:
		rv, cmd := b.fileViewport.Update(msg)
		b.fileViewport = rv.(*vp.ViewportBubble)
		cmds = append(cmds, cmd)
	case treeState:
		l, cmd := b.list.Update(msg)
		b.list = l
		cmds = append(cmds, cmd)
	}

	return b, tea.Batch(cmds...)
}

func (b *Bubble) View() string {
	switch b.state {
	case treeState:
		return b.list.View()
	case errorState:
		return b.error.ViewWithPrefix(b.style, "Error")
	case fileState:
		return b.fileViewport.View()
	default:
		return ""
	}
}

func (b *Bubble) loadFile() tea.Cmd {
	return func() tea.Msg {
		i := b.list.SelectedItem()
		if i == nil {
			return nil
		}
		f, ok := i.(item)
		if !ok {
			return nil
		}
		if !f.Mode().IsFile() || f.File == nil {
			return types.ErrMsg{types.ErrInvalidFile}
		}
		bin, err := f.File.IsBinary()
		if err != nil {
			return types.ErrMsg{err}
		}
		if bin {
			return types.ErrMsg{types.ErrBinaryFile}
		}
		c, err := f.File.Contents()
		if err != nil {
			return types.ErrMsg{err}
		}
		return fileMsg{
			content: c,
		}
	}
}

func (b *Bubble) renderFile(m fileMsg) string {
	s := strings.Builder{}
	c := m.content
	if len(strings.Split(c, "\n")) > types.MaxDiffLines {
		s.WriteString(types.ErrFileTooLarge.Error())
	} else {
		lexer := lexers.Match(b.path)
		lang := ""
		if lexer != nil && lexer.Config() != nil {
			lang = lexer.Config().Name
		}
		formatter := &gansi.CodeBlockElement{
			Code:     c,
			Language: lang,
		}
		r := strings.Builder{}
		err := formatter.Render(&r, types.RenderCtx)
		if err != nil {
			s.WriteString(err.Error())
		} else {
			s.WriteString(r.String())
		}
	}
	return b.style.TreeFileContent.Copy().Width(b.width - b.widthMargin).Render(s.String())
}
