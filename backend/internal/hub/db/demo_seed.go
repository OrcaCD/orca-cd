package db

import (
	"context"
	"errors"
	"time"

	"github.com/OrcaCD/orca-cd/internal/hub/auth"
	"github.com/OrcaCD/orca-cd/internal/hub/crypto"
	"github.com/OrcaCD/orca-cd/internal/hub/models"
	"github.com/OrcaCD/orca-cd/internal/version"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	demoSeedUserID         = "demo-user"
	demoSeedUserEmail      = "demo@orcacd.dev"
	demoSeedUserPassword   = "demo-password"
	demoSeedUserName       = "Demo User"
	demoSeedMemberID       = "demo-member"
	demoSeedMemberEmail    = "sam@orcacd.dev"
	demoSeedMemberName     = "Sam Example"
	demoSeedAgentID        = "demo-agent"
	demoSeedAgentName      = "Prod Server"
	demoSeedAgentKeyID     = "demo-agent-key"
	demoSeedAgentTwoID     = "demo-agent-staging"
	demoSeedAgentTwoName   = "Staging Server"
	demoSeedAgentTwoKeyID  = "demo-agent-staging-key"
	demoSeedRepositoryID   = "demo-repository"
	demoSeedRepositoryName = "OrcaCD/demo-app"
	demoSeedRepositoryURL  = "https://github.com/OrcaCD/demo-app"

	demoSeedSignedRepositoryID   = "demo-repository-signed"
	demoSeedSignedRepositoryName = "OrcaCD/demo-infrastructure"
	demoSeedSignedRepositoryURL  = "https://github.com/OrcaCD/demo-infrastructure"

	demoSeedCommitSigningKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAILzLKC57O0T9rFN/f1l8+X4tAzHb1b8+LMaAJoDFAQ9/"
)

var ErrDemoOnExistingDatabase = errors.New("demo mode cannot be enabled on a database that contains non-demo users, use an empty data directory")

const demoWebComposeFile = `services:
  web:
    image: ghcr.io/orcacd/demo-app:1.4.2
    ports:
      - "8080:80"
    restart: unless-stopped
`

const demoWebPreviousComposeFile = `services:
  web:
    image: ghcr.io/orcacd/demo-app:1.4.1
    ports:
      - "8080:80"
    restart: unless-stopped
`

const demoMonitoringComposeFile = `services:
  prometheus:
    image: prom/prometheus:v3.5.0
    ports:
      - "9090:9090"
    restart: unless-stopped
  grafana:
    image: grafana/grafana:12.1.0
    ports:
      - "3000:3000"
    depends_on:
      - prometheus
    restart: unless-stopped
`

const demoWorkerComposeFile = `services:
  worker:
    image: ghcr.io/orcacd/demo-worker:0.9.0
    restart: unless-stopped
`

func seedDemoData(db *gorm.DB) error {
	ctx := context.Background()

	// Refuse to run on databases that hold real users. The demo users are part of
	// the seed and do not count, so older demo databases still get the newer entities.
	realUserCount, err := gorm.G[models.User](db).
		Where("id NOT IN ?", []string{demoSeedUserID, demoSeedMemberID}).
		Count(ctx, "*")
	if err != nil {
		return err
	}
	if realUserCount > 0 {
		return ErrDemoOnExistingDatabase
	}

	return db.Transaction(func(tx *gorm.DB) error {
		user, err := seedDemoUsers(ctx, tx)
		if err != nil {
			return err
		}

		if err := seedDemoAgents(ctx, tx); err != nil {
			return err
		}

		if err := seedDemoRepositories(ctx, tx, user.Id); err != nil {
			return err
		}

		if err := seedDemoApplications(ctx, tx); err != nil {
			return err
		}

		if err := seedDemoApplicationEvents(ctx, tx, user); err != nil {
			return err
		}

		if err := seedDemoNotifications(ctx, tx); err != nil {
			return err
		}

		return seedDemoAuditLogs(ctx, tx, user.Id)
	})
}

func seedDemoUsers(ctx context.Context, tx *gorm.DB) (*models.User, error) {
	hashedPassword, err := auth.HashPassword(demoSeedUserPassword)
	if err != nil {
		return nil, err
	}

	user := models.User{
		Id:                     demoSeedUserID,
		Email:                  demoSeedUserEmail,
		Name:                   demoSeedUserName,
		Role:                   models.UserRoleAdmin,
		PasswordChangeRequired: false,
		PasswordHash:           &hashedPassword,
	}
	if err := gorm.G[models.User](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &user); err != nil {
		return nil, err
	}

	// No password, so this account cannot be used to log in
	member := models.User{
		Id:    demoSeedMemberID,
		Email: demoSeedMemberEmail,
		Name:  demoSeedMemberName,
		Role:  models.UserRoleUser,
	}
	if err := gorm.G[models.User](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &member); err != nil {
		return nil, err
	}

	resolvedUser, err := gorm.G[models.User](tx).Where("email = ?", demoSeedUserEmail).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("failed to resolve demo user after seeding")
		}
		return nil, err
	}

	return &resolvedUser, nil
}

func seedDemoAgents(ctx context.Context, tx *gorm.DB) error {
	now := time.Now()
	staleSeen := now.Add(-52 * time.Hour)

	agents := []models.Agent{
		{
			Id:            demoSeedAgentID,
			Name:          crypto.EncryptedString(demoSeedAgentName),
			KeyId:         crypto.EncryptedString(demoSeedAgentKeyID),
			Icon:          "server",
			Status:        models.AgentStatusOnline,
			LastSeen:      &now,
			Version:       version.Version,
			DockerVersion: "28.3.2",
		},
		{
			Id:            demoSeedAgentTwoID,
			Name:          crypto.EncryptedString(demoSeedAgentTwoName),
			KeyId:         crypto.EncryptedString(demoSeedAgentTwoKeyID),
			Icon:          "server",
			Status:        models.AgentStatusOffline,
			LastSeen:      &staleSeen,
			Version:       version.Version,
			DockerVersion: "27.5.1",
		},
	}

	for i := range agents {
		if err := gorm.G[models.Agent](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &agents[i]); err != nil {
			return err
		}
	}

	if err := tx.Model(&models.Agent{}).
		Where("id IN ?", []string{demoSeedAgentID, demoSeedAgentTwoID}).
		UpdateColumn("version", version.Version).Error; err != nil {
		return err
	}

	return tx.Model(&models.Agent{}).
		Where("id = ?", demoSeedAgentID).
		UpdateColumns(map[string]any{"status": models.AgentStatusOnline, "last_seen": now}).Error
}

func seedDemoRepositories(ctx context.Context, tx *gorm.DB, createdBy string) error {
	now := time.Now()
	lastSynced := now.Add(-12 * time.Minute)
	pollingInterval := 5 * time.Minute

	repositories := []models.Repository{
		{
			Id:                                demoSeedRepositoryID,
			Name:                              demoSeedRepositoryName,
			Url:                               demoSeedRepositoryURL,
			Provider:                          models.GitHub,
			AuthMethod:                        models.AuthMethodNone,
			SyncType:                          models.SyncTypeManual,
			SyncStatus:                        models.SyncStatusSuccess,
			LastSyncedAt:                      &lastSynced,
			CreatedBy:                         createdBy,
			GitHubActionsOIDCAllowRepoSync:    true,
			GitHubActionsOIDCAllowImageSync:   true,
			GitHubActionsOIDCAllowedBranches:  []string{},
			GitHubActionsOIDCAllowedWorkflows: []string{},
			CommitSigningKeys:                 []string{},
		},
		{
			Id:                                demoSeedSignedRepositoryID,
			Name:                              demoSeedSignedRepositoryName,
			Url:                               demoSeedSignedRepositoryURL,
			Provider:                          models.GitHub,
			AuthMethod:                        models.AuthMethodNone,
			SyncType:                          models.SyncTypePolling,
			SyncStatus:                        models.SyncStatusSuccess,
			PollingInterval:                   &pollingInterval,
			LastSyncedAt:                      &lastSynced,
			CreatedBy:                         createdBy,
			GitHubActionsOIDCEnabled:          true,
			GitHubActionsOIDCAllowRepoSync:    true,
			GitHubActionsOIDCAllowImageSync:   true,
			GitHubActionsOIDCAllowedBranches:  []string{"main"},
			GitHubActionsOIDCAllowedWorkflows: []string{"deploy.yml"},
			RequireSignedCommits:              true,
			CommitSigningKeys:                 []string{demoSeedCommitSigningKey},
		},
	}

	for i := range repositories {
		if err := gorm.G[models.Repository](tx, clause.OnConflict{DoNothing: true}).Select("*").Create(ctx, &repositories[i]); err != nil {
			return err
		}
	}

	return nil
}

func seedDemoApplications(ctx context.Context, tx *gorm.DB) error {
	now := time.Now()
	syncedAt := now.Add(-12 * time.Minute)
	staleSyncedAt := now.Add(-3 * 24 * time.Hour)
	syncError := "failed to fetch compose file: remote: repository not found"
	syncSource := models.ApplicationEventSourceRepositoryPolling
	manualSource := models.ApplicationEventSourceManual
	webhookSecret := crypto.EncryptedString("demo-image-webhook-secret")

	apps := []models.Application{
		{
			Id:                       "demo-app-web",
			Name:                     crypto.EncryptedString("Demo Web App"),
			NameHash:                 crypto.BlindIndex(models.NormalizeName("Demo Web App")),
			Icon:                     "globe",
			RepositoryId:             demoSeedRepositoryID,
			AgentId:                  demoSeedAgentID,
			SyncStatus:               models.Synced,
			HealthStatus:             models.Healthy,
			Branch:                   "main",
			Commit:                   "3f9c2a7d41b86e05c1a9d7f3e2b4a60819cd5e77",
			CommitMessage:            "chore: Bump demo-app to 1.4.2",
			LastSyncedAt:             &syncedAt,
			LastSyncSource:           &manualSource,
			Path:                     "docker-compose.yml",
			ComposeFile:              crypto.EncryptedString(demoWebComposeFile),
			PreviousComposeFile:      crypto.EncryptedString(demoWebPreviousComposeFile),
			ImagePollEnabled:         true,
			ImagePollIntervalSeconds: 300,
			DeleteOldImages:          true,
			ImageWebhookSecret:       &webhookSecret,
		},
		{
			Id:                       "demo-app-monitoring",
			Name:                     crypto.EncryptedString("Monitoring Stack"),
			NameHash:                 crypto.BlindIndex(models.NormalizeName("Monitoring Stack")),
			Icon:                     "chart-line",
			RepositoryId:             demoSeedSignedRepositoryID,
			AgentId:                  demoSeedAgentID,
			SyncStatus:               models.OutOfSync,
			HealthStatus:             models.Unhealthy,
			Branch:                   "main",
			Commit:                   "b81e4c0a9d2f7365e1c48a0f6d93b27a5e04c1d8",
			CommitMessage:            "feat: Add Grafana to the monitoring stack",
			LastSyncedAt:             &syncedAt,
			LastSyncSource:           &syncSource,
			Path:                     "monitoring/compose.yml",
			ComposeFile:              crypto.EncryptedString(demoMonitoringComposeFile),
			PreviousComposeFile:      crypto.EncryptedString(""),
			ImagePollIntervalSeconds: 120,
		},
		{
			Id:                       "demo-app-worker",
			Name:                     crypto.EncryptedString("Background Worker"),
			NameHash:                 crypto.BlindIndex(models.NormalizeName("Background Worker")),
			Icon:                     "box",
			RepositoryId:             demoSeedRepositoryID,
			AgentId:                  demoSeedAgentTwoID,
			SyncStatus:               models.UnknownSync,
			HealthStatus:             models.Stopped,
			Branch:                   "main",
			Commit:                   "a47d90e2c6b1583f0e9d4a7c21b65e8f03d9a4b6",
			CommitMessage:            "fix: Retry failed jobs with backoff",
			LastSyncedAt:             &staleSyncedAt,
			LastSyncError:            &syncError,
			LastSyncSource:           &syncSource,
			Path:                     "worker/compose.yml",
			ComposeFile:              crypto.EncryptedString(demoWorkerComposeFile),
			PreviousComposeFile:      crypto.EncryptedString(""),
			ImagePollIntervalSeconds: 120,
		},
	}

	for i := range apps {
		if err := gorm.G[models.Application](tx, clause.OnConflict{DoNothing: true}).Select("*").Create(ctx, &apps[i]); err != nil {
			return err
		}
	}

	return nil
}

type demoEvent struct {
	id            string
	applicationId string
	eventType     models.ApplicationEventType
	source        models.ApplicationEventSource
	status        models.ApplicationEventStatus
	actorUser     bool
	commitHash    string
	commitMessage string
	errorMessage  string
	ago           time.Duration
	duration      time.Duration
}

func seedDemoApplicationEvents(ctx context.Context, tx *gorm.DB, actor *models.User) error {
	now := time.Now()

	events := []demoEvent{
		{
			id: "demo-event-web-created", applicationId: "demo-app-web",
			eventType: models.ApplicationEventDeployment, source: models.ApplicationEventSourceApplicationCreated,
			status: models.ApplicationEventSucceeded, actorUser: true,
			commitHash: "9d1e5b3a07c24f68a1e0d3b5c7f92a46e8b01d35", commitMessage: "feat: Initial demo-app release",
			ago: 6 * 24 * time.Hour, duration: 14 * time.Second,
		},
		{
			id: "demo-event-web-image", applicationId: "demo-app-web",
			eventType: models.ApplicationEventImageUpdate, source: models.ApplicationEventSourceImagePolling,
			status:     models.ApplicationEventSucceeded,
			commitHash: "9d1e5b3a07c24f68a1e0d3b5c7f92a46e8b01d35", commitMessage: "feat: Initial demo-app release",
			ago: 2 * 24 * time.Hour, duration: 31 * time.Second,
		},
		{
			id: "demo-event-web-sync", applicationId: "demo-app-web",
			eventType: models.ApplicationEventCommitSync, source: models.ApplicationEventSourceRepositoryWebhook,
			status:     models.ApplicationEventSucceeded,
			commitHash: "3f9c2a7d41b86e05c1a9d7f3e2b4a60819cd5e77", commitMessage: "chore: Bump demo-app to 1.4.2",
			ago: 3 * time.Hour, duration: 9 * time.Second,
		},
		{
			id: "demo-event-web-nochange", applicationId: "demo-app-web",
			eventType: models.ApplicationEventCommitSync, source: models.ApplicationEventSourceManual,
			status: models.ApplicationEventNoChange, actorUser: true,
			commitHash: "3f9c2a7d41b86e05c1a9d7f3e2b4a60819cd5e77", commitMessage: "chore: Bump demo-app to 1.4.2",
			ago: 12 * time.Minute, duration: 2 * time.Second,
		},
		{
			id: "demo-event-monitoring-created", applicationId: "demo-app-monitoring",
			eventType: models.ApplicationEventDeployment, source: models.ApplicationEventSourceApplicationCreated,
			status: models.ApplicationEventSucceeded, actorUser: true,
			commitHash: "5c0a8e2f91d3476b0a8e1d4c6f32b975e1a0d843", commitMessage: "feat: Add Prometheus",
			ago: 4 * 24 * time.Hour, duration: 22 * time.Second,
		},
		{
			id: "demo-event-monitoring-deploy", applicationId: "demo-app-monitoring",
			eventType: models.ApplicationEventDeployment, source: models.ApplicationEventSourceGitHubActions,
			status:       models.ApplicationEventFailed,
			commitHash:   "b81e4c0a9d2f7365e1c48a0f6d93b27a5e04c1d8",
			errorMessage: "service grafana is unhealthy: container exited with code 1",
			ago:          47 * time.Minute, duration: 41 * time.Second,
			commitMessage: "feat: Add Grafana to the monitoring stack",
		},
		{
			id: "demo-event-worker-sync", applicationId: "demo-app-worker",
			eventType: models.ApplicationEventCommitSync, source: models.ApplicationEventSourceRepositoryPolling,
			status:       models.ApplicationEventFailed,
			errorMessage: "failed to fetch compose file: remote: repository not found",
			ago:          3 * 24 * time.Hour, duration: 3 * time.Second,
		},
	}

	for i := range events {
		e := events[i]
		createdAt := now.Add(-e.ago)
		completedAt := createdAt.Add(e.duration)

		event := models.ApplicationEvent{
			Id:            e.id,
			ApplicationId: e.applicationId,
			Type:          e.eventType,
			Source:        e.source,
			Status:        e.status,
			CompletedAt:   &completedAt,
		}
		if e.actorUser {
			event.ActorUserId = &actor.Id
			event.ActorName = &actor.Name
		}
		if e.commitHash != "" {
			event.CommitHash = &e.commitHash
		}
		if e.commitMessage != "" {
			event.CommitMessage = &e.commitMessage
		}
		if e.errorMessage != "" {
			event.ErrorMessage = &e.errorMessage
		}

		if err := gorm.G[models.ApplicationEvent](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &event); err != nil {
			return err
		}

		// BeforeCreate stamps the current time, so backdate to spread the history out
		if err := tx.Model(&models.ApplicationEvent{}).
			Where("id = ?", e.id).
			UpdateColumns(map[string]any{"created_at": createdAt, "updated_at": completedAt}).Error; err != nil {
			return err
		}
	}

	return nil
}

func seedDemoNotifications(ctx context.Context, tx *gorm.DB) error {
	notifications := []models.Notification{
		{
			Id:              "demo-notification-discord",
			Name:            crypto.EncryptedString("Team Discord"),
			Enabled:         true,
			AllApplications: true,
			AllAgents:       true,
			AllRepositories: true,
			Events: []models.NotificationEvent{
				models.NotificationEventDeploymentSucceeded,
				models.NotificationEventDeploymentFailed,
				models.NotificationEventAgentOffline,
				models.NotificationEventAgentOnline,
			},
			Status: models.NotificationStatusSuccess,
			Type:   models.NotificationTypeDiscord,
			Config: crypto.EncryptedString(`{"token":"demo-token","webhookId":"123456789012345678"}`),
		},
		{
			Id:              "demo-notification-slack",
			Name:            crypto.EncryptedString("Slack alerts"),
			Enabled:         true,
			AllApplications: true,
			AllRepositories: true,
			Events: []models.NotificationEvent{
				models.NotificationEventSyncFailed,
				models.NotificationEventHealthUnhealthy,
				models.NotificationEventHealthRecovered,
				models.NotificationEventRepositorySyncFailed,
			},
			Status: models.NotificationStatusError,
			Type:   models.NotificationTypeSlack,
			Config: crypto.EncryptedString(`{"webhookUrl":"https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX"}`),
		},
	}

	for i := range notifications {
		if err := gorm.G[models.Notification](tx, clause.OnConflict{DoNothing: true}).Select("*").Create(ctx, &notifications[i]); err != nil {
			return err
		}
	}

	return nil
}

func seedDemoAuditLogs(ctx context.Context, tx *gorm.DB, userID string) error {
	now := time.Now()

	entries := []struct {
		id         string
		eventType  string
		targetType string
		targetId   string
		ago        time.Duration
	}{
		{"demo-audit-agent", "created", "agent", demoSeedAgentID, 7 * 24 * time.Hour},
		{"demo-audit-repository", "created", "repository", demoSeedRepositoryID, 6*24*time.Hour + 2*time.Hour},
		{"demo-audit-application", "created", "application", "demo-app-web", 6 * 24 * time.Hour},
		{"demo-audit-signed-repository", "created", "repository", demoSeedSignedRepositoryID, 4*24*time.Hour + time.Hour},
		{"demo-audit-monitoring", "created", "application", "demo-app-monitoring", 4 * 24 * time.Hour},
		{"demo-audit-webhook", "enabled-image-webhook", "application", "demo-app-web", 2 * 24 * time.Hour},
		{"demo-audit-user", "created", "user", demoSeedMemberID, 24 * time.Hour},
	}

	for i := range entries {
		e := entries[i]
		userId := userID
		targetId := e.targetId
		entry := models.AuditLog{
			Id:         e.id,
			UserId:     &userId,
			EventType:  e.eventType,
			TargetType: e.targetType,
			TargetId:   &targetId,
		}
		if err := gorm.G[models.AuditLog](tx, clause.OnConflict{DoNothing: true}).Create(ctx, &entry); err != nil {
			return err
		}

		createdAt := now.Add(-e.ago)
		if err := tx.Model(&models.AuditLog{}).
			Where("id = ?", e.id).
			UpdateColumns(map[string]any{"created_at": createdAt, "updated_at": createdAt}).Error; err != nil {
			return err
		}
	}

	return nil
}
