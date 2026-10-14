package sse

// Topic identifies a resource the frontend can refetch after an update event sent over SSE.
type Topic struct {
	url string
}

const apiPrefix = "/api/v1"

var (
	TopicAgents       = Topic{apiPrefix + "/agents"}
	TopicApplications = Topic{apiPrefix + "/applications"}
	// TopicApplicationActivity is the cross-application deployment summary, which
	// changes with every recorded or completed application event.
	TopicApplicationActivity = Topic{apiPrefix + "/applications/activity"}
	TopicRepositories        = Topic{apiPrefix + "/repositories"}
	TopicNotifications       = Topic{apiPrefix + "/notifications"}
	TopicAdminUsers          = Topic{apiPrefix + "/admin/users"}
	TopicAdminOIDCProvider   = Topic{apiPrefix + "/admin/oidc-providers"}
)

func TopicApplicationEvents(applicationID string) Topic {
	return Topic{apiPrefix + "/applications/" + applicationID + "/events"}
}

func (t Topic) URL() string {
	return t.url
}
