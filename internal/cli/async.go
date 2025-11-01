package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"opperator/internal/ipc"
)

type AsyncListOptions struct {
	Status  string
	Origin  string
	Session string
	Client  string
}

func ListAsyncTasks(opts AsyncListOptions) error {
	client, err := ipc.NewClientFromRegistry("local")
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	tasks, err := client.ListToolTasks()
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No async tasks recorded")
		return nil
	}

	filtered := make([]*ipc.ToolTask, 0, len(tasks))
	statusFilter := strings.ToLower(strings.TrimSpace(opts.Status))
	originFilter := strings.ToLower(strings.TrimSpace(opts.Origin))
	sessionFilter := strings.TrimSpace(opts.Session)
	clientFilter := strings.TrimSpace(opts.Client)
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if statusFilter != "" && strings.ToLower(strings.TrimSpace(task.Status)) != statusFilter {
			continue
		}
		if originFilter != "" && strings.ToLower(strings.TrimSpace(task.Origin)) != originFilter {
			continue
		}
		if sessionFilter != "" && strings.TrimSpace(task.SessionID) != sessionFilter {
			continue
		}
		if clientFilter != "" && strings.TrimSpace(task.ClientID) != clientFilter {
			continue
		}
		filtered = append(filtered, task)
	}
	if len(filtered) == 0 {
		fmt.Println("No async tasks matched the provided filters")
		return nil
	}

	sort.Slice(filtered, func(i, j int) bool {
		ii := strings.TrimSpace(filtered[i].UpdatedAt)
		jj := strings.TrimSpace(filtered[j].UpdatedAt)
		if ii == "" || jj == "" {
			return filtered[i].ID < filtered[j].ID
		}
		it, errI := time.Parse(time.RFC3339Nano, ii)
		jt, errJ := time.Parse(time.RFC3339Nano, jj)
		if errI != nil || errJ != nil {
			return filtered[i].ID < filtered[j].ID
		}
		return jt.Before(it)
	})

	fmt.Printf("%-36s %-10s %-8s %-8s %-8s %-10s %-10s %-20s\n", "TASK ID", "STATUS", "ORIGIN", "CLIENT", "SESSION", "CALL", "MODE", "TOOL")
	fmt.Printf("%-36s %-10s %-8s %-8s %-8s %-10s %-10s %-20s\n", strings.Repeat("-", 36), strings.Repeat("-", 10), strings.Repeat("-", 8), strings.Repeat("-", 8), strings.Repeat("-", 8), strings.Repeat("-", 10), strings.Repeat("-", 10), strings.Repeat("-", 20))

	for _, task := range filtered {
		status := strings.TrimSpace(task.Status)
		if status == "" {
			status = "pending"
		}
		origin := orDash(task.Origin)
		client := orDash(task.ClientID)
		session := orDash(task.SessionID)
		call := orDash(task.CallID)
		mode := strings.TrimSpace(task.Mode)
		if mode == "" {
			mode = "tool"
		}
		tool := strings.TrimSpace(task.ToolName)
		if tool == "" {
			tool = "-"
		}
		fmt.Printf("%-36s %-10s %-8s %-8s %-8s %-10s %-10s %-20s\n", task.ID, status, origin, client, session, call, mode, tool)
	}

	return nil
}

func ShowAsyncTask(id string) error {
	client, err := ipc.NewClientFromRegistry("local")
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	task, err := client.GetToolTask(id)
	if err != nil {
		return err
	}
	if task == nil {
		fmt.Println("Task not found")
		return nil
	}

	printTaskDetails(task)
	return nil
}

func DeleteAsyncTask(id string) error {
	client, err := ipc.NewClientFromRegistry("local")
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such file") {
			return fmt.Errorf("daemon is not running. Start it with: ./opperator daemon")
		}
		return err
	}
	defer client.Close()

	if err := client.DeleteToolTask(id); err != nil {
		return err
	}
	fmt.Printf("Deleted async task %s\n", id)
	return nil
}

func printTaskDetails(task *ipc.ToolTask) {
	fmt.Println("Async Task Details")
	fmt.Println("-------------------")
	fmt.Printf("ID:          %s\n", task.ID)
	fmt.Printf("Status:      %s\n", strings.TrimSpace(task.Status))
	fmt.Printf("Tool:        %s\n", strings.TrimSpace(task.ToolName))
	fmt.Printf("Mode:        %s\n", strings.TrimSpace(task.Mode))
	fmt.Printf("Origin:      %s\n", orDash(task.Origin))
	fmt.Printf("Client ID:   %s\n", orDash(task.ClientID))
	fmt.Printf("Session ID:  %s\n", orDash(task.SessionID))
	fmt.Printf("Call ID:     %s\n", orDash(task.CallID))
	fmt.Printf("Agent:       %s\n", orDash(task.AgentName))
	fmt.Printf("Command:     %s\n", orDash(task.CommandName))
	fmt.Printf("Created At:  %s\n", orDash(task.CreatedAt))
	fmt.Printf("Updated At:  %s\n", orDash(task.UpdatedAt))
	fmt.Printf("Completed At:%s\n", orDash(task.CompletedAt))

	if trimmed := strings.TrimSpace(task.Args); trimmed != "" {
		fmt.Printf("Args:        %s\n", trimmed)
	}
	if trimmed := strings.TrimSpace(task.Metadata); trimmed != "" {
		fmt.Printf("Metadata:    %s\n", trimmed)
	}
	if trimmed := strings.TrimSpace(task.Error); trimmed != "" {
		fmt.Printf("Error:       %s\n", trimmed)
	}
	if trimmed := strings.TrimSpace(task.Result); trimmed != "" {
		fmt.Printf("Result:\n%s\n", trimmed)
	}
	if len(task.Progress) > 0 {
		fmt.Println("Progress:")
		for _, entry := range task.Progress {
			ts := orDash(entry.Timestamp)
			text := strings.TrimSpace(entry.Text)
			status := strings.TrimSpace(entry.Status)
			meta := strings.TrimSpace(entry.Metadata)
			fmt.Printf("  - %s", ts)
			if status != "" {
				fmt.Printf(" [%s]", status)
			}
			if text != "" {
				fmt.Printf(" %s", text)
			}
			if meta != "" {
				fmt.Printf(" (meta: %s)", meta)
			}
			fmt.Println()
		}
	}
}

func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return strings.TrimSpace(value)
}
