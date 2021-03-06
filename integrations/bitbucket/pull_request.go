package main

import (
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/pinpt/agent/integrations/pkg/objsender"
	"github.com/pinpt/agent/integrations/pkg/repoprojects"
	"github.com/pinpt/agent/rpcdef"

	"github.com/hashicorp/go-hclog"
	"github.com/pinpt/agent/integrations/bitbucket/api"
	"github.com/pinpt/agent/integrations/pkg/commonrepo"
	"github.com/pinpt/integration-sdk/sourcecode"
)

func (s *Integration) exportPullRequestsForRepo(ctx *repoprojects.ProjectCtx, repo commonrepo.Repo) (res []rpcdef.GitRepoFetchPR, rerr error) {

	pullRequestSender, err := ctx.Session(sourcecode.PullRequestModelName)
	if err != nil {
		rerr = err
		return
	}
	commitsSender, err := ctx.Session(sourcecode.PullRequestCommitModelName)
	if err != nil {
		rerr = err
		return
	}

	logger := ctx.Logger.With("repo", repo.NameWithOwner)
	logger.Info("exporting")

	// export changed pull requests
	var pullRequestsErr error
	pullRequestsInitial := make(chan []sourcecode.PullRequest)
	go func() {
		defer close(pullRequestsInitial)
		if err := s.exportPullRequestsRepo(logger, repo, pullRequestSender, pullRequestsInitial, pullRequestSender.LastProcessedTime()); err != nil {
			pullRequestsErr = err
		}
	}()

	// export comments, reviews, commits concurrently
	pullRequestsForComments := make(chan []sourcecode.PullRequest, 10)
	pullRequestsForCommits := make(chan []sourcecode.PullRequest, 10)

	var errMu sync.Mutex
	setErr := func(err error) {
		logger.Error("failed repo export", "e", err)
		errMu.Lock()
		defer errMu.Unlock()
		if rerr == nil {
			rerr = err
		}
		// drain all pull requests on error
		for range pullRequestsForComments {
		}
		for range pullRequestsForCommits {
		}
	}

	go func() {
		for item := range pullRequestsInitial {
			pullRequestsForComments <- item
			pullRequestsForCommits <- item
		}
		close(pullRequestsForComments)
		close(pullRequestsForCommits)

		if pullRequestsErr != nil {
			setErr(pullRequestsErr)
		}
	}()

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := s.exportPullRequestsComments(logger, pullRequestSender, repo, pullRequestsForComments)
		if err != nil {
			setErr(fmt.Errorf("error getting comments %s", err))
		}
	}()

	// set commits on the rp and then send the pr
	wg.Add(1)
	go func() {
		defer wg.Done()
		for prs := range pullRequestsForCommits {
			for _, pr := range prs {
				commits, err := s.exportPullRequestCommits(logger, repo, pr.RefID)
				if err != nil {
					setErr(fmt.Errorf("error getting commits %s", err))
					return
				}

				if len(commits) > 0 {
					meta := rpcdef.GitRepoFetchPR{}
					repoID := s.qc.IDs.CodeRepo(repo.ID)
					meta.ID = s.qc.IDs.CodePullRequest(repoID, pr.RefID)
					meta.RefID = pr.RefID
					meta.URL = pr.URL
					meta.BranchName = pr.BranchName
					meta.LastCommitSHA = commits[0].Sha
					res = append(res, meta)
				}
				for ind := len(commits) - 1; ind >= 0; ind-- {
					pr.CommitShas = append(pr.CommitShas, commits[ind].Sha)
				}

				pr.CommitIds = s.qc.IDs.CodeCommits(pr.RepoID, pr.CommitShas)
				if len(pr.CommitShas) == 0 {
					logger.Info("found PullRequest with no commits (ignoring it)", "repo", repo.NameWithOwner, "pr_ref_id", pr.RefID, "pr.url", pr.URL)
				} else {
					pr.BranchID = s.qc.IDs.CodeBranch(pr.RepoID, pr.BranchName, pr.CommitShas[0])
				}

				if err = pullRequestSender.Send(&pr); err != nil {
					setErr(err)
					return
				}

				for _, c := range commits {
					c.BranchID = pr.BranchID
					err := commitsSender.Send(c)
					if err != nil {
						setErr(err)
						return
					}
				}
			}
		}
	}()
	wg.Wait()
	return
}

func (s *Integration) exportPullRequestsRepo(logger hclog.Logger, repo commonrepo.Repo, sender *objsender.Session, pullRequests chan []sourcecode.PullRequest, lastProcessed time.Time) error {
	return api.PaginateNewerThan(logger, lastProcessed, func(log hclog.Logger, parameters url.Values, stopOnUpdatedAt time.Time) (api.PageInfo, error) {
		pi, res, err := api.PullRequestPage(s.qc, sender, repo.ID, repo.NameWithOwner, parameters, stopOnUpdatedAt)
		if err != nil {
			return pi, err
		}
		if err = sender.SetTotal(pi.Total); err != nil {
			return pi, err
		}
		pullRequests <- res
		return pi, nil
	})
}
