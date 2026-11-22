package websocket

import "slices"

// CapabilityContainerMonitoring is advertised by agents that can list, inspect
// and stream logs and stats of an application's containers.
const CapabilityContainerMonitoring = "container-monitoring"

const (
	maxReportedCapabilities     = 32
	maxReportedCapabilityLength = 64
)

func (c *Client) SetCapabilities(reported []string) {
	if len(reported) > maxReportedCapabilities {
		reported = reported[:maxReportedCapabilities]
	}
	capabilities := make([]string, 0, len(reported))
	for _, capability := range reported {
		if len(capability) <= maxReportedCapabilityLength {
			capabilities = append(capabilities, capability)
		}
	}
	c.capabilities.Store(&capabilities)
}

func (c *Client) hasCapability(capability string) bool {
	capabilities := c.capabilities.Load()
	return capabilities != nil && slices.Contains(*capabilities, capability)
}

func (h *Hub) HasCapability(agentID, capability string) bool {
	c, ok := h.GetClient(agentID)
	return ok && c.hasCapability(capability)
}
