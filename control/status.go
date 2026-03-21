package control

import (
	"go.olrik.dev/subspace/stats"
	"go.olrik.dev/subspace/upstream"
)

// UpstreamInfo describes an upstream proxy for health checking.
type UpstreamInfo struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

// StatusResponse is the JSON response from the /status endpoint.
type StatusResponse struct {
	Upstreams   map[string]UpstreamStatus `json:"upstreams"`
	Connections ConnectionStatus          `json:"connections"`
	Pool        *upstream.PoolStats       `json:"pool,omitempty"`
}

// UpstreamStatus describes the health and stats of a single upstream.
type UpstreamStatus struct {
	Type    string              `json:"type"`
	Address string              `json:"address"`
	Healthy bool                `json:"healthy"`
	Latency string              `json:"latency"`
	Stats   *stats.UpstreamStats `json:"stats,omitempty"`
}

// ConnectionStatus summarizes proxy connection counts.
type ConnectionStatus struct {
	Total  int64 `json:"total"`
	Active int64 `json:"active"`
}
