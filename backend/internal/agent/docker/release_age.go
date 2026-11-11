package docker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	messages "github.com/OrcaCD/orca-cd/internal/proto"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/distribution/reference"
	godigest "github.com/opencontainers/go-digest"
)

// releaseAgeLabel overrides the minimum release age for one service, e.g. "0"
// for a first-party image or "7d" for a database. It wins over the hub's settings.
const releaseAgeLabel = "orca-cd.minimum-release-age"

// ReleaseAgePolicy holds back updated images until they were pushed to their
// registry at least MinimumAge ago. This gives maintainers and scanners time to
// remove a malicious version before it is deployed.
type ReleaseAgePolicy struct {
	MinimumAge time.Duration
	// ServiceMinimumAge overrides MinimumAge per compose service.
	ServiceMinimumAge map[string]time.Duration
	// AllowedRegistries skip the check.
	AllowedRegistries []string
	// WebhookImages are the images an image pull webhook announced. When set,
	// only they are updated, without the check, as their publisher triggered it.
	WebhookImages []string
}

func ReleaseAgePolicyFromProto(policy *messages.ReleaseAgePolicy, webhookImages []string) ReleaseAgePolicy {
	var services map[string]time.Duration
	for service, seconds := range policy.GetServiceMinimumSeconds() {
		if services == nil {
			services = make(map[string]time.Duration)
		}
		services[service] = time.Duration(seconds) * time.Second
	}
	return ReleaseAgePolicy{
		MinimumAge:        time.Duration(policy.GetMinimumSeconds()) * time.Second,
		ServiceMinimumAge: services,
		AllowedRegistries: policy.GetAllowedRegistries(),
		WebhookImages:     webhookImages,
	}
}

func (p ReleaseAgePolicy) equal(other ReleaseAgePolicy) bool {
	return p.MinimumAge == other.MinimumAge &&
		maps.Equal(p.ServiceMinimumAge, other.ServiceMinimumAge) &&
		slices.Equal(p.AllowedRegistries, other.AllowedRegistries) &&
		slices.Equal(p.WebhookImages, other.WebhookImages)
}

// minimumAge returns the minimum release age of the service, and whether its
// label set it.
func (p ReleaseAgePolicy) minimumAge(service composetypes.ServiceConfig) (time.Duration, bool, error) {
	if value, ok := service.Labels[releaseAgeLabel]; ok {
		age, err := parseReleaseAge(value)
		return age, true, err
	}
	if age, ok := p.ServiceMinimumAge[service.Name]; ok {
		return age, false, nil
	}
	return p.MinimumAge, false, nil
}

// evaluateUpdate decides whether the service's image may be updated to digest
// and records why in update. It returns whether the age of the digest was
// checked, which means it has to be pulled by that digest.
func (c *Client) evaluateUpdate(ctx context.Context, appID string, policy ReleaseAgePolicy, service composetypes.ServiceConfig, digest string, update *messages.ImageUpdate) (checked bool) {
	update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED

	minimumAge, fromLabel, err := policy.minimumAge(service)
	update.MinimumAgeFromLabel = fromLabel
	if err != nil {
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED
		update.Reason = fmt.Sprintf("invalid %s label: %v", releaseAgeLabel, err)
		return false
	}
	update.MinimumAgeSeconds = int64(minimumAge / time.Second)
	if minimumAge <= 0 {
		return false
	}

	if len(policy.WebhookImages) > 0 {
		if !slices.ContainsFunc(policy.WebhookImages, func(image string) bool { return sameRepository(image, service.Image) }) {
			update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED
			update.Reason = "not announced by the image pull webhook, image polling updates it"
		}
		return false
	}

	registry := registryHost(service.Image)
	if slices.Contains(policy.AllowedRegistries, registry) {
		return false
	}

	since := "pushed"
	pushed, err := getPushTime(ctx, c.cli, service.Image, digest)
	if err != nil {
		// The age counts from when the agent first saw the digest instead. That
		// is never before the push and can't be backdated by the publisher.
		c.log.Debug().Err(err).Str("image", service.Image).Msg("no push time, using the first time the digest was seen")
		pushed = c.firstSeenAt(appID, service.Name, digest)
		update.FirstSeen = true
		since = "first seen"
	}
	eligible := pushed.Add(minimumAge)
	update.PushedAt = pushed.Unix()
	update.EligibleAt = eligible.Unix()
	if time.Now().Before(eligible) {
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING
		update.Reason = fmt.Sprintf("%s at %s, waiting until %s (minimum release age %s)",
			since, pushed.UTC().Format(time.RFC3339), eligible.UTC().Format(time.RFC3339), formatReleaseAge(minimumAge))
		return false
	}
	return true
}

type firstSeenDigest struct {
	digest string
	at     time.Time
}

// firstSeenAt returns when the agent first saw digest as the new image of the
// service. It is kept in memory, so it starts over when the agent restarts,
// which only makes the update wait longer.
func (c *Client) firstSeenAt(appID, service, digest string) time.Time {
	c.releaseAgeMu.Lock()
	defer c.releaseAgeMu.Unlock()

	key := appID + "/" + service
	if seen, ok := c.firstSeen[key]; ok && seen.digest == digest {
		return seen.at
	}
	if c.firstSeen == nil {
		c.firstSeen = make(map[string]firstSeenDigest)
	}
	now := time.Now()
	c.firstSeen[key] = firstSeenDigest{digest: digest, at: now}
	return now
}

// isHeldBack reports whether an update waits for the release age policy.
func isHeldBack(update *messages.ImageUpdate) bool {
	return update.Status == messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_WAITING ||
		update.Status == messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED ||
		update.Status == messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED
}

// sameRepository reports whether two image references name the same
// repository, ignoring their tags.
func sameRepository(a, b string) bool {
	namedA, errA := reference.ParseNormalizedNamed(a)
	namedB, errB := reference.ParseNormalizedNamed(b)
	return errA == nil && errB == nil && namedA.Name() == namedB.Name()
}

// pinnedRef returns imageRef pinned to digest, e.g. "redis@sha256:…".
func pinnedRef(imageRef, digest string) (string, error) {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return "", err
	}
	pinned, err := reference.WithDigest(reference.TrimNamed(named), godigest.Digest(digest))
	if err != nil {
		return "", err
	}
	return pinned.String(), nil
}

// parseReleaseAge parses durations like "3d", "1d12h" or "90m". Go durations
// have no days, so a leading day count is handled here.
func parseReleaseAge(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return 0, nil
	}
	var age time.Duration
	if days, rest, ok := strings.Cut(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid number of days in %q", value)
		}
		age = time.Duration(n) * 24 * time.Hour
		value = rest
	}
	if value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return 0, err
		}
		age += duration
	}
	if age < 0 {
		return 0, errors.New("release age must not be negative")
	}
	return age, nil
}

func formatReleaseAge(age time.Duration) string {
	day := 24 * time.Hour
	if age%day == 0 {
		return fmt.Sprintf("%dd", age/day)
	}
	return age.String()
}
