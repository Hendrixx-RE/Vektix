package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Hendrixx-RE/Vektix/internal/config"
	"github.com/Hendrixx-RE/Vektix/internal/format"
	"github.com/Hendrixx-RE/Vektix/internal/index"
	"github.com/Hendrixx-RE/Vektix/internal/ollama"
	"github.com/Hendrixx-RE/Vektix/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// IndexProgressMsg conveys incremental progress from the index pipeline.
type IndexProgressMsg struct {
	Progress index.Progress
}

// IndexTickMsg advances the progress spinner animation.
type IndexTickMsg struct{}

// IndexDoneMsg marks the completion of an indexing or sync run.
type IndexDoneMsg struct {
	Result  *index.Result
	Err     error
	Elapsed time.Duration
}

// IndexModel manages the interactive progress display during indexing/syncing.
type IndexModel struct {
	Running      bool
	Mode         index.Mode
	Roots        []string
	Progress     index.Progress
	Result       *index.Result
	Err          error
	StartTime    time.Time
	Elapsed      time.Duration
	SpinnerIndex int
	CancelFn     context.CancelFunc
	progChan     chan index.Progress
}

// NewIndexModel returns an idle IndexModel.
func NewIndexModel() IndexModel {
	return IndexModel{
		Running: false,
	}
}

func tickSpinnerCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return IndexTickMsg{}
	})
}

func listenProgress(ch <-chan index.Progress) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		p, ok := <-ch
		if !ok {
			return nil
		}
		return IndexProgressMsg{Progress: p}
	}
}

// Start initiates an asynchronous index or sync pipeline with live progress messaging.
func (m *IndexModel) Start(cfg *config.Config, roots []string, mode index.Mode) tea.Cmd {
	return m.startWithOptions(cfg, roots, mode, false)
}

// StartTransient initiates an ephemeral indexing run for a single directory.
func (m *IndexModel) StartTransient(cfg *config.Config, root string) tea.Cmd {
	return m.startWithOptions(cfg, []string{root}, index.ModeIndex, true)
}

func (m *IndexModel) startWithOptions(cfg *config.Config, roots []string, mode index.Mode, transient bool) tea.Cmd {
	m.Running = true
	m.Mode = mode
	m.Roots = roots
	m.Progress = index.Progress{}
	m.Result = nil
	m.Err = nil
	startTime := time.Now()
	m.StartTime = startTime
	m.Elapsed = 0
	m.SpinnerIndex = 0
	m.progChan = make(chan index.Progress, 128)

	ctx, cancel := context.WithCancel(context.Background())
	m.CancelFn = cancel

	ch := m.progChan

	runCmd := func() tea.Msg {
		defer close(ch)

		dataDir, err := config.ExpandPath(cfg.General.DataDir)
		if err != nil {
			return IndexDoneMsg{Err: fmt.Errorf("resolving data_dir: %w", err), Elapsed: time.Since(startTime)}
		}

		if err := os.MkdirAll(dataDir, 0755); err != nil {
			return IndexDoneMsg{Err: fmt.Errorf("creating data_dir %s: %w", dataDir, err), Elapsed: time.Since(startTime)}
		}

		st, err := store.NewPersistentDB(index.StorePath(dataDir))
		if err != nil {
			return IndexDoneMsg{Err: fmt.Errorf("opening vector store: %w", err), Elapsed: time.Since(startTime)}
		}

		cli := ollama.NewClient(ollama.Options{
			Host:         cfg.Ollama.Host,
			EmbedTimeout: time.Duration(cfg.Ollama.Timeouts.EmbedBatchSeconds) * time.Second,
		})

		engine := index.NewEngine(cfg, st, cli, dataDir)
		engine.Transient = transient
		engine.OnProgress = func(p index.Progress) {
			select {
			case ch <- p:
			default:
			}
		}

		res, runErr := engine.Run(ctx, roots, mode)
		elapsed := time.Since(startTime)

		return IndexDoneMsg{
			Result:  res,
			Err:     runErr,
			Elapsed: elapsed,
		}
	}

	return tea.Batch(runCmd, listenProgress(ch), tickSpinnerCmd())
}

// Update processes index-related events and keypresses.
func (m *IndexModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case IndexProgressMsg:
		m.Progress = msg.Progress
		if m.Running && m.progChan != nil {
			return listenProgress(m.progChan)
		}
		return nil

	case IndexTickMsg:
		if m.Running {
			m.SpinnerIndex++
			m.Elapsed = time.Since(m.StartTime)
			return tickSpinnerCmd()
		}
		return nil

	case IndexDoneMsg:
		m.Running = false
		m.Result = msg.Result
		m.Err = msg.Err
		m.Elapsed = msg.Elapsed
		if m.CancelFn != nil {
			m.CancelFn()
			m.CancelFn = nil
		}
		return nil

	case tea.KeyMsg:
		if msg.String() == "esc" || msg.String() == "q" {
			if m.Running && m.CancelFn != nil {
				m.CancelFn()
				m.CancelFn = nil
				m.Running = false
			}
		}
	}

	return nil
}

// View renders the indexing card with stats and status.
func (m IndexModel) View(width int, theme Theme) string {
	var rows []string

	modeName := "Indexing"
	if m.Mode == index.ModeSync {
		modeName = "Syncing"
	} else if m.Mode == index.ModeReindex {
		modeName = "Reindexing"
	}

	if m.Running {
		spinner := spinnerFrames[m.SpinnerIndex%len(spinnerFrames)]
		title := fmt.Sprintf("%s %s in progress...", spinner, modeName)
		rows = append(rows, theme.IndexTitle.Render(title))
		rows = append(rows, "")

		stats := []string{
			fmt.Sprintf("%s %s", theme.IndexStatLabel.Render("Scanned:"), theme.IndexStatValue.Render(format.HumanInt(m.Progress.Scanned))),
			fmt.Sprintf("%s %s", theme.IndexStatLabel.Render("Indexed:"), theme.IndexStatValue.Render(format.HumanInt(m.Progress.Indexed))),
			fmt.Sprintf("%s %s", theme.IndexStatLabel.Render("Chunks:"), theme.IndexStatValue.Render(format.HumanInt(m.Progress.Chunks))),
		}
		if m.Progress.Quarantined > 0 {
			stats = append(stats, fmt.Sprintf("%s %s", theme.WarningText.Render("Quarantined:"), theme.WarningText.Render(format.HumanInt(m.Progress.Quarantined))))
		}
		rows = append(rows, strings.Join(stats, "   "))
		rows = append(rows, "")
		rows = append(rows, theme.KeyHintDesc.Render("Press [esc] to cancel"))
	} else if m.Err != nil {
		rows = append(rows, theme.ErrorText.Render(fmt.Sprintf("✗ %s failed: %v", modeName, m.Err)))
		rows = append(rows, "")
		rows = append(rows, theme.KeyHintDesc.Render("Press [esc] or [enter] to return"))
	} else if m.Result != nil {
		res := m.Result
		rows = append(rows, theme.SuccessText.Render(fmt.Sprintf("✓ %s complete (%s)", modeName, format.FormatDuration(m.Elapsed))))
		rows = append(rows, "")

		summary := fmt.Sprintf("Scanned %s files — %d added, %d updated, %d unchanged, %d removed (%s chunks)",
			format.HumanInt(len(res.Files)+res.Unchanged), res.Added, res.Updated, res.Unchanged, res.Removed, format.HumanInt(res.Chunks))
		rows = append(rows, theme.UserInput.Render(summary))
		if len(res.Quarantined) > 0 {
			rows = append(rows, theme.WarningText.Render(fmt.Sprintf("⚠ %d file(s) quarantined (malformed/unreadable)", len(res.Quarantined))))
		}
		rows = append(rows, "")
		rows = append(rows, theme.KeyHintDesc.Render("Press [esc] or [enter] to return to query"))
	}

	boxWidth := width - 4
	if boxWidth < 40 {
		boxWidth = 40
	}

	return theme.PickerBox.Width(boxWidth).Render(strings.Join(rows, "\n"))
}
