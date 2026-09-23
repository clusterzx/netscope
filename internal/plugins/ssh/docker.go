package ssh

import "netscope/internal/dockercli"

// Parsers of the docker sections (shared with other collectors).
var (
	parseDockerPS     = dockercli.ParsePS
	parseDockerImages = dockercli.ParseImages
	resolveImageIDs   = dockercli.ResolveImageIDs
	parseDockerPorts  = dockercli.ParsePorts
	stateFromStatus   = dockercli.StateFromStatus
	parseHumanSize    = dockercli.ParseHumanSize
)
