package sse

import "github.com/OrcaCD/orca-cd/internal/hub/models"

// Topic identifies a resource the frontend can refetch after an update event sent over SSE.
// Topics name the resource they are about, so clients only get events for
// resources they are allowed to read.
type Topic struct {
	url string
	// resource is the type the topic is about. Topics without one are sent to everyone.
	resource models.PermissionResource
	// resourceID narrows the topic to a single resource. Without it, the topic is
	// sent to everyone who can read at least one resource of the type.
	resourceID string
	adminOnly  bool
}

const apiPrefix = "/api/v1"

var (
	TopicAgents       = Topic{url: apiPrefix + "/agents", resource: models.ResourceAgent}
	TopicApplications = Topic{url: apiPrefix + "/applications", resource: models.ResourceApplication}
	// TopicApplicationActivity is the cross-application deployment summary. It
	// lives outside /applications, because the frontend matches topics by prefix
	// and every application status report would otherwise refetch it.
	TopicApplicationActivity = Topic{url: apiPrefix + "/activity", resource: models.ResourceApplication}
	TopicRepositories        = Topic{url: apiPrefix + "/repositories", resource: models.ResourceRepository}
	TopicNotifications       = Topic{url: apiPrefix + "/notifications", resource: models.ResourceNotification}
	TopicAdminUsers          = Topic{url: apiPrefix + "/admin/users", adminOnly: true}
	TopicAdminOIDCProvider   = Topic{url: apiPrefix + "/admin/oidc-providers", adminOnly: true}
	TopicAdminRoles          = Topic{url: apiPrefix + "/admin/roles", adminOnly: true}
	// TopicProfile makes clients reload their own profile, which holds their permissions.
	TopicProfile = Topic{url: apiPrefix + "/auth/profile"}
)

func TopicApplicationEvents(applicationID string) Topic {
	return Topic{
		url:        apiPrefix + "/applications/" + applicationID + "/events",
		resource:   models.ResourceApplication,
		resourceID: applicationID,
	}
}

// For narrows the topic to the resource with the given ID.
func (t Topic) For(id string) Topic {
	t.resourceID = id
	return t
}

func (t Topic) URL() string {
	return t.url
}

// Resource returns the resource type and ID the topic is about, if any.
func (t Topic) Resource() (models.PermissionResource, string) {
	return t.resource, t.resourceID
}

func (t Topic) AdminOnly() bool {
	return t.adminOnly
}
