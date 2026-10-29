package healthhistory

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

type Range string

const (
	Range24h Range = "24h"
	Range7d  Range = "7d"
	Range30d Range = "30d"
)

type rangeConfig struct {
	duration time.Duration
	bucket   time.Duration
}

var ranges = map[Range]rangeConfig{
	Range24h: {duration: 24 * time.Hour, bucket: time.Hour},
	Range7d:  {duration: 7 * 24 * time.Hour, bucket: 3 * time.Hour},
	Range30d: {duration: 30 * 24 * time.Hour, bucket: 12 * time.Hour},
}

// ParseRange validates a statistics range.
func ParseRange(value string) (Range, bool) {
	r := Range(value)
	_, ok := ranges[r]
	return r, ok
}

const (
	// MaxIncidents bounds the incidents returned with the statistics. Counts
	// always cover every incident in the range.
	MaxIncidents = 100
	// maxChanges bounds the health changes loaded for one range, so a flapping
	// application cannot make the statistics arbitrarily expensive. When the
	// range has more changes, only the latest ones are used.
	maxChanges = 10_000
	// deployIncidentGrace is how long after a deployment or image update an
	// incident is still attributed to it.
	deployIncidentGrace = 5 * time.Minute
	// maxRunningEventAge bounds the attribution to a deployment or image update
	// that is still running, so a stuck one is not blamed forever.
	maxRunningEventAge = 30 * time.Minute
	// eventQueryMargin widens the database filter for application events. Their
	// timestamps are stored in the hub's local time, so the exact comparison
	// happens in Go.
	eventQueryMargin = 24 * time.Hour
)

// Stats summarizes an application's health over a range. Durations are in
// seconds. Time before the application's first health change is not tracked
// and counts neither as data nor as a gap.
type Stats struct {
	Range         Range         `json:"range"`
	From          time.Time     `json:"from"`
	To            time.Time     `json:"to"`
	BucketSeconds int64         `json:"bucketSeconds"`
	Current       *CurrentState `json:"current"`
	// Truncated is set when the range has more than maxChanges changes. The
	// time before the oldest loaded change is then not tracked.
	Truncated bool `json:"truncated"`
	// Availability is the healthy share of the observed time in percent, nil
	// when nothing was observed in the range.
	Availability           *float64   `json:"availability"`
	HealthySeconds         int64      `json:"healthySeconds"`
	DowntimeSeconds        int64      `json:"downtimeSeconds"`
	NoDataSeconds          int64      `json:"noDataSeconds"`
	IncidentCount          int        `json:"incidentCount"`
	LongestIncidentSeconds int64      `json:"longestIncidentSeconds"`
	DataGapCount           int        `json:"dataGapCount"`
	Buckets                []Bucket   `json:"buckets"`
	Incidents              []Incident `json:"incidents"`
}

type CurrentState struct {
	Status models.HealthStatus `json:"status"`
	Since  time.Time           `json:"since"`
}

// Bucket is one bar of the availability chart. Status is the most severe
// failing status in the bucket; without failures it is unknown when the
// bucket has more time without data than healthy time. It is nil when the
// bucket lies entirely before the application's first health change.
type Bucket struct {
	Start            time.Time            `json:"start"`
	End              time.Time            `json:"end"`
	Status           *models.HealthStatus `json:"status"`
	HealthySeconds   int64                `json:"healthySeconds"`
	DegradedSeconds  int64                `json:"degradedSeconds"`
	UnhealthySeconds int64                `json:"unhealthySeconds"`
	StoppedSeconds   int64                `json:"stoppedSeconds"`
	NoDataSeconds    int64                `json:"noDataSeconds"`
}

// Incident is a period in which the application was degraded, unhealthy or
// stopped, or a data gap in which its health was unknown. Consecutive failing
// changes form one incident with the most severe status.
type Incident struct {
	Status          models.HealthStatus        `json:"status"`
	StartedAt       time.Time                  `json:"startedAt"`
	EndedAt         *time.Time                 `json:"endedAt"`
	DurationSeconds int64                      `json:"durationSeconds"`
	Reason          *models.HealthChangeReason `json:"reason,omitempty"`
	Services        []models.ServiceHealth     `json:"services"`
	Trigger         *IncidentTrigger           `json:"trigger,omitempty"`
}

// IncidentTrigger is the deployment or image update an incident started
// during or shortly after.
type IncidentTrigger struct {
	Type       models.ApplicationEventType `json:"type"`
	CommitHash *string                     `json:"commitHash,omitempty"`
}

type segment struct {
	status   models.HealthStatus
	reason   *models.HealthChangeReason
	services []models.ServiceHealth
	start    time.Time
	end      time.Time
	// recorded is false before the application's first health change.
	recorded bool
}

// GetStats computes the health statistics of an application for the range
// ending at now.
func GetStats(ctx context.Context, applicationID string, r Range, now time.Time) (Stats, error) {
	cfg, ok := ranges[r]
	if !ok {
		return Stats{}, errors.New("invalid health statistics range")
	}
	now = now.UTC()
	from := now.Add(-cfg.duration)

	changes, err := gorm.G[models.ApplicationHealthChange](db.DB).
		Where("application_id = ? AND changed_at >= ? AND changed_at <= ?", applicationID, from, now).
		Order("changed_at DESC, id DESC").
		Limit(maxChanges + 1).
		Find(ctx)
	if err != nil {
		return Stats{}, err
	}
	truncated := len(changes) > maxChanges
	if truncated {
		changes = changes[:maxChanges]
	}
	slices.Reverse(changes)
	if !truncated {
		initial, err := gorm.G[models.ApplicationHealthChange](db.DB).
			Where("application_id = ? AND changed_at < ?", applicationID, from).
			Order("changed_at DESC, id DESC").
			First(ctx)
		switch {
		case err == nil:
			changes = append([]models.ApplicationHealthChange{initial}, changes...)
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return Stats{}, err
		}
	}

	segments := buildSegments(changes, from, now)
	stats := Stats{
		Range:         r,
		From:          from,
		To:            now,
		BucketSeconds: int64(cfg.bucket / time.Second),
		Truncated:     truncated,
		Buckets:       buildBuckets(segments, from, now, cfg.bucket),
	}
	if len(changes) > 0 {
		latest := changes[len(changes)-1]
		stats.Current = &CurrentState{Status: latest.Status, Since: latest.ChangedAt}
	}

	var healthy, downtime, noData time.Duration
	for _, s := range segments {
		d := overlap(s.start, s.end, from, now)
		switch {
		case s.status == models.Healthy:
			healthy += d
		case isFailing(s.status):
			downtime += d
		case s.recorded:
			noData += d
		}
	}
	stats.HealthySeconds = seconds(healthy)
	stats.DowntimeSeconds = seconds(downtime)
	stats.NoDataSeconds = seconds(noData)
	if observed := healthy + downtime; observed > 0 {
		availability := float64(healthy) / float64(observed) * 100
		stats.Availability = &availability
	}

	incidents := buildIncidents(segments, now)
	for _, incident := range incidents {
		if incident.Status == models.UnknownHealth {
			stats.DataGapCount++
			continue
		}
		stats.IncidentCount++
		stats.LongestIncidentSeconds = max(stats.LongestIncidentSeconds, incident.DurationSeconds)
	}
	slices.Reverse(incidents)
	if len(incidents) > MaxIncidents {
		incidents = incidents[:MaxIncidents]
	}
	if err := attachTriggers(ctx, applicationID, incidents, now); err != nil {
		return Stats{}, err
	}
	stats.Incidents = incidents
	return stats, nil
}

// buildSegments turns ordered health changes into contiguous segments covering
// from..now. The first segment starts at the first change's real time when it
// precedes the range, so incidents keep their actual start.
func buildSegments(changes []models.ApplicationHealthChange, from, now time.Time) []segment {
	var segments []segment
	if len(changes) == 0 || changes[0].ChangedAt.After(from) {
		end := now
		if len(changes) > 0 {
			end = changes[0].ChangedAt
		}
		segments = append(segments, segment{status: models.UnknownHealth, start: from, end: end})
	}
	for i, change := range changes {
		end := now
		if i+1 < len(changes) {
			end = changes[i+1].ChangedAt
		}
		segments = append(segments, segment{
			status:   change.Status,
			reason:   change.Reason,
			services: change.Services,
			start:    change.ChangedAt,
			end:      end,
			recorded: true,
		})
	}
	return segments
}

// buildBuckets sums the ordered, contiguous segments per bucket in a single
// pass over both.
func buildBuckets(segments []segment, from, now time.Time, size time.Duration) []Bucket {
	count := int(now.Sub(from) / size)
	buckets := make([]Bucket, count)
	for i := range buckets {
		buckets[i].Start = from.Add(time.Duration(i) * size)
		buckets[i].End = buckets[i].Start.Add(size)
	}

	first := 0
	for i := range buckets {
		b := &buckets[i]
		for first < len(segments) && !segments[first].end.After(b.Start) {
			first++
		}
		for _, s := range segments[first:] {
			if !s.start.Before(b.End) {
				break
			}
			d := seconds(overlap(s.start, s.end, b.Start, b.End))
			switch s.status {
			case models.Healthy:
				b.HealthySeconds += d
			case models.Degraded:
				b.DegradedSeconds += d
			case models.Unhealthy:
				b.UnhealthySeconds += d
			case models.Stopped:
				b.StoppedSeconds += d
			default:
				if s.recorded {
					b.NoDataSeconds += d
				}
			}
		}

		var status models.HealthStatus
		switch {
		case b.StoppedSeconds > 0:
			status = models.Stopped
		case b.UnhealthySeconds > 0:
			status = models.Unhealthy
		case b.DegradedSeconds > 0:
			status = models.Degraded
		case b.NoDataSeconds > b.HealthySeconds:
			status = models.UnknownHealth
		case b.HealthySeconds > 0:
			status = models.Healthy
		default:
			continue
		}
		b.Status = &status
	}
	return buckets
}

// buildIncidents returns incidents and recorded data gaps in chronological
// order.
func buildIncidents(segments []segment, now time.Time) []Incident {
	// Non-nil so a range without incidents serializes as an empty list.
	incidents := []Incident{}
	for _, s := range segments {
		if !s.recorded || s.status == models.Healthy || !s.end.After(s.start) {
			continue
		}

		var endedAt *time.Time
		if s.end.Before(now) {
			end := s.end
			endedAt = &end
		}

		failing := isFailing(s.status)
		if last := len(incidents) - 1; failing && last >= 0 && isFailing(incidents[last].Status) && incidents[last].EndedAt != nil && incidents[last].EndedAt.Equal(s.start) {
			incident := &incidents[last]
			incident.Status = moreSevere(incident.Status, s.status)
			incident.Services = mergeServices(incident.Services, s.services)
			incident.EndedAt = endedAt
			incident.DurationSeconds = seconds(s.end.Sub(incident.StartedAt))
			continue
		}

		incident := Incident{
			Status:          s.status,
			StartedAt:       s.start,
			EndedAt:         endedAt,
			DurationSeconds: seconds(s.end.Sub(s.start)),
			Services:        mergeServices(nil, s.services),
		}
		if !failing {
			incident.Reason = s.reason
		}
		incidents = append(incidents, incident)
	}
	return incidents
}

// attachTriggers links failing incidents to the deployment or image update
// they started during or shortly after. Events that changed nothing are
// ignored.
func attachTriggers(ctx context.Context, applicationID string, incidents []Incident, now time.Time) error {
	earliest := now
	for _, incident := range incidents {
		if isFailing(incident.Status) && incident.StartedAt.Before(earliest) {
			earliest = incident.StartedAt
		}
	}
	if earliest.Equal(now) {
		return nil
	}

	events, err := gorm.G[models.ApplicationEvent](db.DB).
		Where("application_id = ? AND type IN ? AND status <> ? AND created_at >= ?",
			applicationID,
			[]models.ApplicationEventType{models.ApplicationEventDeployment, models.ApplicationEventImageUpdate},
			models.ApplicationEventNoChange,
			earliest.Add(-eventQueryMargin),
		).
		Order("created_at DESC, id DESC").
		Find(ctx)
	if err != nil {
		return err
	}

	for i := range incidents {
		incident := &incidents[i]
		if !isFailing(incident.Status) {
			continue
		}
		for _, event := range events {
			if event.CreatedAt.After(incident.StartedAt) {
				continue
			}
			if causedIncident(event, incident.StartedAt) {
				incident.Trigger = &IncidentTrigger{Type: event.Type, CommitHash: event.CommitHash}
			}
			// Only the latest event before the incident can have caused it.
			break
		}
	}
	return nil
}

// causedIncident reports whether an incident started while the event was
// running or within the grace period after it completed.
func causedIncident(event models.ApplicationEvent, startedAt time.Time) bool {
	switch {
	case event.CompletedAt != nil:
		return !startedAt.After(event.CompletedAt.Add(deployIncidentGrace))
	case event.Status == models.ApplicationEventRunning:
		return !startedAt.After(event.CreatedAt.Add(maxRunningEventAge))
	default:
		return false
	}
}

func isFailing(status models.HealthStatus) bool {
	return status == models.Degraded || status == models.Unhealthy || status == models.Stopped
}

func severity(status models.HealthStatus) int {
	switch status {
	case models.Stopped:
		return 3
	case models.Unhealthy:
		return 2
	case models.Degraded:
		return 1
	default:
		return 0
	}
}

func moreSevere(a, b models.HealthStatus) models.HealthStatus {
	if severity(b) > severity(a) {
		return b
	}
	return a
}

// mergeServices adds services to merged, keeping the most severe status per
// service, sorted by name.
func mergeServices(merged, services []models.ServiceHealth) []models.ServiceHealth {
	result := slices.Clone(merged)
	if result == nil {
		result = []models.ServiceHealth{}
	}
	for _, service := range services {
		i := slices.IndexFunc(result, func(s models.ServiceHealth) bool { return s.Name == service.Name })
		if i < 0 {
			result = append(result, service)
			continue
		}
		result[i].Status = moreSevere(result[i].Status, service.Status)
	}
	slices.SortFunc(result, func(a, b models.ServiceHealth) int { return cmp.Compare(a.Name, b.Name) })
	return result
}

func overlap(start, end, from, to time.Time) time.Duration {
	if start.Before(from) {
		start = from
	}
	if end.After(to) {
		end = to
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}

func seconds(d time.Duration) int64 {
	return int64(d.Round(time.Second) / time.Second)
}
