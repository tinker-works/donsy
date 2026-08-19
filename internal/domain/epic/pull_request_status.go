package epic

type PullRequestStatus string

const (
	PullRequestOpen   PullRequestStatus = "open"
	PullRequestClosed PullRequestStatus = "closed"
	PullRequestMerged PullRequestStatus = "merged"
)
