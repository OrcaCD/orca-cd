package websocket

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/hub/notifications"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// agentOfflineGracePeriod is how long an agent may stay disconnected before an
// offline notification is sent. Short reconnects (agent restarts, network
// blips, token rotation) therefore stay silent.
const agentOfflineGracePeriod = 60 * time.Second

// agentPresenceNotifier turns agent connects and disconnects into offline and
// online notifications. The state is kept in memory only, so a hub restart
// forgets which agents were reported offline.
type agentPresenceNotifier struct {
	mu        sync.Mutex
	delay     time.Duration
	afterFunc func(time.Duration, func()) (stop func() bool)
	notify    func(agentId string, event models.NotificationEvent)
	// pending holds the timer token of agents whose offline notification is
	// still waiting for the grace period to elapse.
	pending map[string]*offlineTimer
	// reportedOffline holds agents for which an offline notification was sent,
	// so only those get an online notification when they reconnect.
	reportedOffline map[string]struct{}
	stopped         bool
}

type offlineTimer struct {
	stop func() bool
}

func newAgentPresenceNotifier(delay time.Duration, notify func(agentId string, event models.NotificationEvent)) *agentPresenceNotifier {
	return &agentPresenceNotifier{
		delay: delay,
		afterFunc: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
		notify:          notify,
		pending:         make(map[string]*offlineTimer),
		reportedOffline: make(map[string]struct{}),
	}
}

// Disconnected starts the grace period after which the agent is reported offline.
func (n *agentPresenceNotifier) Disconnected(agentId string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.stopped {
		return
	}
	if _, alreadyReported := n.reportedOffline[agentId]; alreadyReported {
		return
	}
	if previous, ok := n.pending[agentId]; ok {
		previous.stop()
	}

	timer := &offlineTimer{}
	n.pending[agentId] = timer
	timer.stop = n.afterFunc(n.delay, func() { n.reportOffline(agentId, timer) })
}

func (n *agentPresenceNotifier) reportOffline(agentId string, timer *offlineTimer) {
	n.mu.Lock()
	// A reconnect or a newer disconnect may have replaced this timer after it
	// already fired; only the current one may report.
	if n.stopped || n.pending[agentId] != timer {
		n.mu.Unlock()
		return
	}
	delete(n.pending, agentId)
	n.reportedOffline[agentId] = struct{}{}
	n.mu.Unlock()

	n.notify(agentId, models.NotificationEventAgentOffline)
}

// Connected cancels a pending offline report and reports the agent online if
// it was previously reported offline.
func (n *agentPresenceNotifier) Connected(agentId string) {
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		return
	}
	if timer, ok := n.pending[agentId]; ok {
		timer.stop()
		delete(n.pending, agentId)
	}
	_, wasReportedOffline := n.reportedOffline[agentId]
	delete(n.reportedOffline, agentId)
	n.mu.Unlock()

	if wasReportedOffline {
		n.notify(agentId, models.NotificationEventAgentOnline)
	}
}

func (n *agentPresenceNotifier) Stop() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stopped = true
	for agentId, timer := range n.pending {
		timer.stop()
		delete(n.pending, agentId)
	}
}

func notifyAgentPresence(agentId string, event models.NotificationEvent, log *zerolog.Logger) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		agent, err := gorm.G[models.Agent](db.DB).Select("id", "name").Where("id = ?", agentId).First(ctx)
		if err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Error().Err(err).Str("agent_id", agentId).Msg("failed to load agent for presence notification")
			}
			return
		}

		message := "Success: agent " + agent.Name.String() + " is back online"
		if event == models.NotificationEventAgentOffline {
			message = "Error: agent " + agent.Name.String() + " is offline"
		}
		notifications.SendForAgent(agentId, event, message, log)
	}()
}
