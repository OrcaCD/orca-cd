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
	// EnforcedMinimumAge is the least age of every image, which neither labels,
	// service overrides nor AllowedRegistries lower. Images of
	// EnforcedExemptRegistries skip it.
	EnforcedMinimumAge       time.Duration
	EnforcedExemptRegistries []string
	// WebhookImages limit the pull to these images, e.g. the ones an image pull
	// webhook announced. They wait for the release age like any other image.
	WebhookImages []string
	// ExcludedServices are compose services whose images image polling skips.
	ExcludedServices []string
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
		MinimumAge:               time.Duration(policy.GetMinimumSeconds()) * time.Second,
		ServiceMinimumAge:        services,
		AllowedRegistries:        policy.GetAllowedRegistries(),
		EnforcedMinimumAge:       time.Duration(policy.GetEnforcedMinimumSeconds()) * time.Second,
		EnforcedExemptRegistries: policy.GetEnforcedExemptRegistries(),
		WebhookImages:            webhookImages,
	}
}

func (p ReleaseAgePolicy) equal(other ReleaseAgePolicy) bool {
	return p.MinimumAge == other.MinimumAge &&
		maps.Equal(p.ServiceMinimumAge, other.ServiceMinimumAge) &&
		slices.Equal(p.AllowedRegistries, other.AllowedRegistries) &&
		p.EnforcedMinimumAge == other.EnforcedMinimumAge &&
		slices.Equal(p.EnforcedExemptRegistries, other.EnforcedExemptRegistries) &&
		slices.Equal(p.WebhookImages, other.WebhookImages) &&
		slices.Equal(p.ExcludedServices, other.ExcludedServices)
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

// enforcedMinimumAge returns the enforced minimum release age of imageRef.
func (p ReleaseAgePolicy) enforcedMinimumAge(imageRef string) time.Duration {
	if slices.Contains(p.EnforcedExemptRegistries, registryHost(imageRef)) {
		return 0
	}
	return p.EnforcedMinimumAge
}

// setMinimumAge records the minimum release age of the service in update and
// returns it. The enforced minimum raises a lower one.
func (p ReleaseAgePolicy) setMinimumAge(service composetypes.ServiceConfig, update *messages.ImageUpdate) (time.Duration, error) {
	age, fromLabel, err := p.minimumAge(service)
	update.MinimumAgeFromLabel = fromLabel
	if err != nil {
		return 0, err
	}
	// MinimumAgeFromLabel stays set, as the label still wins over the settings.
	if enforced := p.enforcedMinimumAge(service.Image); enforced > age {
		age = enforced
		update.MinimumAgeEnforced = true
	}
	update.MinimumAgeSeconds = int64(age / time.Second)
	return age, nil
}

// exempt reports whether imageRef only waits for the enforced minimum, as it
// is from an allowed registry.
func (p ReleaseAgePolicy) exempt(imageRef string) bool {
	return slices.Contains(p.AllowedRegistries, registryHost(imageRef))
}

// setExemptMinimumAge records the minimum release age of an exempt image in
// update and returns it.
func (p ReleaseAgePolicy) setExemptMinimumAge(imageRef string, update *messages.ImageUpdate) time.Duration {
	age := p.enforcedMinimumAge(imageRef)
	update.MinimumAgeSeconds = int64(age / time.Second)
	update.MinimumAgeFromLabel = false
	update.MinimumAgeEnforced = age > 0
	return age
}

// reportMinimumAge records in update the minimum release age an update of the
// image of the service would wait for.
func (p ReleaseAgePolicy) reportMinimumAge(service composetypes.ServiceConfig, update *messages.ImageUpdate) {
	if _, err := p.setMinimumAge(service, update); err == nil && p.exempt(service.Image) {
		p.setExemptMinimumAge(service.Image, update)
	}
}

// evaluateUpdate decides whether the service's image may be updated to digest
// and records why in update. It returns whether the age of the digest was
// checked, which means it has to be pulled by that digest.
func (c *Client) evaluateUpdate(ctx context.Context, appID string, policy ReleaseAgePolicy, service composetypes.ServiceConfig, digest string, update *messages.ImageUpdate) (checked bool) {
	update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_UPDATED

	minimumAge, err := policy.setMinimumAge(service, update)
	if err != nil {
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_BLOCKED
		update.Reason = fmt.Sprintf("invalid %s label: %v", releaseAgeLabel, err)
		return false
	}
	if slices.Contains(policy.ExcludedServices, service.Name) {
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED
		update.Reason = "image polling is off for this service, a manual pull or webhook updates it"
		return false
	}
	if minimumAge <= 0 {
		return false
	}

	if len(policy.WebhookImages) > 0 &&
		!slices.ContainsFunc(policy.WebhookImages, func(image string) bool { return sameTag(image, service.Image) }) {
		update.Status = messages.ImageUpdateStatus_IMAGE_UPDATE_STATUS_SKIPPED
		update.Reason = "not part of this pull, image polling updates it"
		return false
	}
	if policy.exempt(service.Image) {
		if minimumAge = policy.setExemptMinimumAge(service.Image, update); minimumAge <= 0 {
			return false
		}
	}

	since := "pushed"
	pushed, err := getPushTime(ctx, c.cli, service.Image, digest)
	if err != nil {
		// The age counts from when the agent first saw the digest instead. That
		// is never before the push and can't be backdated by the publisher.
		c.log.Debug().Err(err).Str("image", service.Image).Msg("no push time, using the first time the digest was seen")
		pushed = c.firstSeenAt(appID, tagKey(service.Image), digest)
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
// image tag of the application. It is kept in memory, so it starts over when the agent restarts,
// which only makes the update wait longer.
func (c *Client) firstSeenAt(appID, image, digest string) time.Time {
	c.releaseAgeMu.Lock()
	defer c.releaseAgeMu.Unlock()

	key := appID + "/" + image
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

// sameTag reports whether two image references name the same repository and
// tag, a missing tag being "latest". A reference without a tag, e.g. one pinned
// to a digest, only matches itself.
func sameTag(a, b string) bool {
	namedA, errA := reference.ParseNormalizedNamed(a)
	namedB, errB := reference.ParseNormalizedNamed(b)
	if errA != nil || errB != nil {
		return false
	}
	return reference.TagNameOnly(namedA).String() == reference.TagNameOnly(namedB).String()
}

// tagKey returns the normalized repository and tag of imageRef, e.g.
// "docker.io/library/redis:latest" for "redis".
func tagKey(imageRef string) string {
	named, err := reference.ParseNormalizedNamed(imageRef)
	if err != nil {
		return imageRef
	}
	return reference.TagNameOnly(named).String()
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

// maxReleaseAge is the longest minimum release age, the same limit the hub
// sets. It also keeps day counts from overflowing a time.Duration.
const maxReleaseAge = 365 * 24 * time.Hour

// parseReleaseAge parses durations like "3d", "1d12h" or "90m". Go durations
// have no days, so a leading day count is handled here.
func parseReleaseAge(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "0" {
		return 0, nil
	}
	tooLong := fmt.Errorf("release age %q is longer than %s", value, formatReleaseAge(maxReleaseAge))
	var age time.Duration
	if days, rest, ok := strings.Cut(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid number of days in %q", value)
		}
		if n > int(maxReleaseAge/(24*time.Hour)) {
			return 0, tooLong
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
	if age > maxReleaseAge {
		return 0, tooLong
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
