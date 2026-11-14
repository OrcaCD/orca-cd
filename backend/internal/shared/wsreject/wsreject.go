// Package wsreject defines the machine-readable reason the hub sends when it
// refuses an agent's WebSocket upgrade. Status codes alone are ambiguous, as
// proxies in front of the hub can return the same ones.
package wsreject

const Header = "X-OrcaCD-Reject-Reason"

const (
	IPNotAllowed     = "ip_not_allowed"
	AlreadyConnected = "already_connected"
)
