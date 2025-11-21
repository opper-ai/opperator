package cli

import "opperator/internal/publish"

// PublishAgent packages an agent directory into a GitHub repository using the wizard flow.
func PublishAgent(agentName string, skipScan bool) error {
	return publish.PublishAgent(agentName, publish.PublishOptions{
		SkipScan: skipScan,
	})
}
