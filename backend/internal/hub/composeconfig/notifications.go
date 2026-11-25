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
// ID. A name has to match exactly one notification.
func (c *Config) ResolveNotifications(ctx context.Context) error {
	if c.notifications == nil {
		return nil
	}
	all, err := gorm.G[models.Notification](db.DB).Select("id", "name").Find(ctx)
	if err != nil {
		return fmt.Errorf("failed to load the notifications: %w", err)
	}
	c.resolved = make([]models.Notification, 0, len(c.notifications))
	var errs []error
	for _, name := range c.notifications {
		var matches []models.Notification
		for _, notification := range all {
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
			errs = append(errs, fmt.Errorf("%s.%s: there is no notification %q", Key, keyNotifications, name))
		default:
			errs = append(errs, fmt.Errorf("%s.%s: %d notifications are named %q, use the ID of one", Key, keyNotifications, len(matches), name))
		}
	}
	return errors.Join(errs...)
}

// SaveNotifications assigns the application to the notifications of the
// config, which ResolveNotifications looked up, or to none when x-orcacd
// stopped setting them. Otherwise it leaves them as they are.
func (c *Config) SaveNotifications(ctx context.Context, app *models.Application) error {
	association := db.DB.WithContext(ctx).Model(app).Association("Notifications")
	switch {
	case c.notifications != nil:
		return association.Replace(c.resolved)
	case c.clearNotifications:
		return association.Clear()
	}
	return nil
}
