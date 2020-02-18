package api

import (
	"fmt"
	"net/url"
	"time"

	"github.com/pinpt/agent/integrations/pkg/commonrepo"
	"github.com/pinpt/agent/pkg/date"
	pstrings "github.com/pinpt/go-common/strings"
	"github.com/pinpt/integration-sdk/sourcecode"
)

func PullRequestReviewsPage(
	qc QueryContext,
	repo commonrepo.Repo,
	pr PullRequest,
	params url.Values) (pi PageInfo, res []*sourcecode.PullRequestReview, err error) {

	qc.Logger.Debug("pull request reviews", "repo", repo.NameWithOwner, "prID", pr.ID, "prIID", pr.IID)

	objectPath := pstrings.JoinURL("projects", repo.ID, "merge_requests", pr.IID, "approvals")

	var rreview struct {
		ID         int64 `json:"id"`
		ApprovedBy []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"approved_by"`
		SuggestedApprovers []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"suggested_approvers"`
		Approvers []struct {
			User struct {
				Username string `json:"username"`
			} `json:"user"`
		} `json:"approvers"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	pi, err = qc.Request(objectPath, params, &rreview)
	if err != nil {
		return
	}

	for _, a := range rreview.ApprovedBy {
		item := &sourcecode.PullRequestReview{}
		item.CustomerID = qc.CustomerID
		item.RefType = qc.RefType
		item.RefID = fmt.Sprint(rreview.ID)
		item.RepoID = qc.IDs.CodeRepo(repo.ID)
		item.PullRequestID = qc.IDs.CodePullRequest(item.RepoID, pr.ID)
		item.State = sourcecode.PullRequestReviewStateApproved

		date.ConvertToModel(rreview.CreatedAt, &item.CreatedDate)

		item.UserRefID = a.User.Username

		res = append(res, item)
	}

	for _, a := range rreview.SuggestedApprovers {
		item := &sourcecode.PullRequestReview{}
		item.CustomerID = qc.CustomerID
		item.RefType = qc.RefType
		item.RefID = fmt.Sprint(rreview.ID)
		item.RepoID = qc.IDs.CodeRepo(repo.ID)
		item.PullRequestID = qc.IDs.CodePullRequest(item.RepoID, pr.ID)
		item.State = sourcecode.PullRequestReviewStatePending

		date.ConvertToModel(rreview.CreatedAt, &item.CreatedDate)

		item.UserRefID = a.User.Username

		res = append(res, item)
	}

	return
}
