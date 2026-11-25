package composeconfig

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/OrcaCD/orca-cd/internal/hub/db"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"gorm.io/gorm"
)

// SettingNotifications is the setting of the notifications the application
// is assigned to. While x-orcacd sets them, they can't be changed through the
// API, also not from the notification.
const SettingNotifications = "notifications"

const (
	keyNotifications = "notifications"
	maxNotifications = 50
)

// parseNotifications reads the list of notifications, each a name or an ID.
func parseNotifications(value any) ([]string, error) {
	const message = "must be a list of at most 50 notification names or IDs"
	items, ok := value.([]any)
	if !ok || len(items) > maxNotifications {
		return nil, errors.New(message)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, errors.New(message)
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names, nil
}

// ResolveNotifications looks up the notifications of the config by name or
// ID, among the notifications that applications can assign themselves to.
// A name has to match exactly one of them. Other notifications are never
// matched, so their names can't be probed.
func (c *Config) ResolveNotifications(ctx context.Context) error {
	if c.notifications == nil {
		return nil
	}
	assignable, err := gorm.G[models.Notification](db.DB).
		Select("id", "name").
		Where("compose_assignable = ?", true).
		Find(ctx)
	if err != nil {
		return fmt.Errorf("failed to load the notifications: %w", err)
	}
	c.resolved = make([]models.Notification, 0, len(c.notifications))
	var errs []error
	for _, name := range c.notifications {
		var matches []models.Notification
		for _, notification := range assignable {
			if notification.Id == name || notification.Name.String() == name {
				matches = append(matches, notification)
			}
		}
		switch len(matches) {
		case 1:
			if !slices.ContainsFunc(c.resolved, func(n models.Notification) bool { return n.Id == matches[0].Id }) {
				c.resolved = append(c.resolved, matches[0])
			}
		case 0:
			errs = append(errs, fmt.Errorf("%s.%s: no notification %s allows compose files to assign applications",
				Key, keyNotifications, quoteKey(name)))
		default:
			errs = append(errs, fmt.Errorf("%s.%s: %d notifications are named %s, use the ID of one",
				Key, keyNotifications, len(matches), quoteKey(name)))
		}
	}
	return joinErrors(errs)
}

// SaveNotifications assigns the application to the notifications of the
// config, which ResolveNotifications looked up, and removes it from the other
// notifications that allow compose files to assign applications. When
// x-orcacd stopped setting the notifications, it removes it from all of
// those. Notifications that don't allow it are left alone.
func (c *Config) SaveNotifications(ctx context.Context, app *models.Application) error {
	if c.notifications == nil && !c.clearNotifications {
		return nil
	}
	return db.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var linked []models.Notification
		if err := tx.Model(app).Where("compose_assignable = ?", true).Association("Notifications").Find(&linked); err != nil {
			return err
		}
		isResolved := func(n models.Notification) bool {
			return slices.ContainsFunc(c.resolved, func(r models.Notification) bool { return r.Id == n.Id })
		}
		isLinked := func(n models.Notification) bool {
			return slices.ContainsFunc(linked, func(l models.Notification) bool { return l.Id == n.Id })
		}
		if remove := slices.DeleteFunc(slices.Clone(linked), isResolved); len(remove) > 0 {
			if err := tx.Model(app).Association("Notifications").Delete(remove); err != nil {
				return err
			}
		}
		if add := slices.DeleteFunc(slices.Clone(c.resolved), isLinked); len(add) > 0 {
			// Omit keeps Append from writing the notifications, which were only
			// loaded partially, e.g. when one was deleted meanwhile.
			return tx.Omit("Notifications.*").Model(app).Association("Notifications").Append(add)
		}
		return nil
	})
}
