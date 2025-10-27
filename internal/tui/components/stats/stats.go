package stats

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss/v2"
	"tui/styles"
)

// Stats is the right-side status panel (simple) with basic process tracking.
type Stats struct {
	w, h  int
	stat  string
	model string

	running int
	stopped int
	crashed int
	total   int

	lastErr     string
	lastUpdated time.Time
}

func (c *Stats) SetSize(w, h int) { c.w, c.h = w, h }

func (c *Stats) SetStatus(s string) { c.stat = s }
func (c *Stats) SetModel(s string)  { c.model = s }

func (c *Stats) SetProcessCounts(running, stopped, crashed, total int) {
	c.running, c.stopped, c.crashed, c.total = running, stopped, crashed, total
	c.lastUpdated = time.Now()
	c.lastErr = ""
}

// SetError records a transient error for display.
func (c *Stats) SetError(err error) {
	if err != nil {
		c.lastErr = err.Error()
	} else {
		c.lastErr = ""
	}
}

func (c *Stats) View() string {
	t := styles.CurrentTheme()
	box := lipgloss.NewStyle().
		Width(c.w).
		Height(max(c.h, 1)).
		Padding(0, 1)

	// Header
	lines := []string{
		t.S().Title.MarginRight(100).Render("Stats"),
	}

	title := t.S().Title.MarginRight(4).Render("     ")

	joinedHoriz := ""
	if c.total > 0 || c.lastErr != "" {
		if c.lastErr != "" {
			lines = append(lines, t.S().Base.Foreground(t.Error).Render("Error: "+c.lastErr))
		}
		if c.total > 0 {
			processStats := []string{
				t.S().Base.Foreground(t.Success).MarginRight(2).Render(fmt.Sprintf("● %d", c.running)),
				t.S().Base.Foreground(t.FgSubtle).MarginRight(2).Render(fmt.Sprintf("● %d", c.stopped)),
				t.S().Base.Foreground(t.Error).MarginRight(0).Render(fmt.Sprintf("● %d", c.crashed)),
			}
			joinedHoriz = lipgloss.JoinHorizontal(lipgloss.Center, processStats...)
		}
	}

	content := lipgloss.JoinHorizontal(lipgloss.Center, title, joinedHoriz)
	return box.Render(content)
}

// FetchCounts queries the local daemon over the unix socket and returns
// counts by status. This duplicates just enough of the protocol to avoid
// importing the parent module.
func FetchCounts() (running, stopped, crashed, total int, _ error) {
	req := struct {
		Type string `json:"type"`
	}{Type: "list"}
	b, _ := json.Marshal(req)

	// Connect to socket
	sock := getSocketPath()
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer conn.Close()

	// Write request with newline
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return 0, 0, 0, 0, err
	}

	// Read single-line response
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return 0, 0, 0, 0, err
		}
		return 0, 0, 0, 0, fmt.Errorf("no response from daemon")
	}

	// Decode
	var resp struct {
		Success   bool          `json:"success"`
		Error     string        `json:"error"`
		Processes []processInfo `json:"processes"`
	}
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return 0, 0, 0, 0, err
	}
	if !resp.Success {
		if resp.Error == "" {
			resp.Error = "unknown error"
		}
		return 0, 0, 0, 0, errors.New(resp.Error)
	}

	// Count
	for _, p := range resp.Processes {
		total++
		switch strings.ToLower(string(p.Status)) {
		case "running":
			running++
		case "crashed":
			crashed++
		default:
			stopped++
		}
	}
	return running, stopped, crashed, total, nil
}

type processInfo struct {
	Name   string      `json:"name"`
	Status processStat `json:"status"`
}

type processStat string

func getSocketPath() string {
	return filepath.Join(os.TempDir(), "opperator.sock")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
