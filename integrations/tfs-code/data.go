package main

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/pinpt/go-common/datetime"

	"github.com/pinpt/agent.next/pkg/date"
	"github.com/pinpt/agent.next/pkg/ids"
	"github.com/pinpt/agent.next/pkg/objsender"
	"github.com/pinpt/agent.next/rpcdef"
	"github.com/pinpt/integration-sdk/agent"
	"github.com/pinpt/integration-sdk/sourcecode"
)

func urlAppend(p1, p2 string) string {
	return strings.TrimSuffix(p1, "/") + "/" + p2
}

func commitURLTemplate(reponame, repoURLPrefix string) string {
	return urlAppend(repoURLPrefix, reponame) + "/commit/@@@sha@@@"
}

func branchURLTemplate(reponame, repoURLPrefix string) string {
	return urlAppend(repoURLPrefix, reponame) + "/tree/@@@branch@@@"
}

func (s *Integration) export() error {
	repoids, projids, err := s.exportReposAndRipSrc()
	if err != nil {
		return err
	}
	// exports api users and then commit users
	if err = s.exportUsers(projids, repoids); err != nil {
		return err
	}
	if err = s.exportPullRequestData(repoids); err != nil {
		return err
	}
	return nil
}

func (s *Integration) fetcfReposAndProjectIDs() ([]*sourcecode.Repo, []string, error) {
	return s.api.FetchRepos(s.conf.Repos, s.conf.Excluded)
}

func (s *Integration) exportReposAndRipSrc() (repoids []string, projids []string, err error) {

	sender := objsender.NewNotIncremental(s.agent, sourcecode.RepoModelName.String())
	repos, projids, err := s.fetcfReposAndProjectIDs()
	if err != nil {
		return
	}
	for _, repo := range repos {
		repoids = append(repoids, repo.RefID)
		if err := sender.Send(repo); err != nil {
			return nil, nil, err
		}
		u, err := url.Parse(repo.URL)
		if err != nil {
			return nil, nil, err
		}
		if s.conf.OverrideGitHostName != "" {
			u.Host = s.conf.OverrideGitHostName
		}
		u.User = url.UserPassword(s.creds.Username, s.creds.Password)
		args := rpcdef.GitRepoFetch{}
		args.RefType = s.conf.reftype
		args.RepoID = s.api.RepoID(repo.RefID)
		args.URL = u.String()
		args.BranchURLTemplate = branchURLTemplate(repo.Name, s.creds.URL)
		args.CommitURLTemplate = commitURLTemplate(repo.Name, s.creds.URL)
		if err = s.agent.ExportGitRepo(args); err != nil {
			panic(err)
		}
	}
	return repoids, projids, sender.Done()
}

func (s *Integration) fetchAllUsers(projids []string, repoids []string) map[string]*sourcecode.User {
	usermap := make(map[string]*sourcecode.User)
	for _, proj := range projids {
		if err := s.api.FetchUsers(proj, usermap); err != nil {
			s.logger.Error("error fetching users", "err", err)
		}
	}
	return usermap
}

func (s *Integration) exportUsers(projids []string, repoids []string) error {
	srcsender := objsender.NewNotIncremental(s.agent, sourcecode.UserModelName.String())
	defer func() {
		if err := srcsender.Done(); err != nil {
			s.logger.Error("error in srcsender", "err", err)
		}
	}()
	usermap := s.fetchAllUsers(projids, repoids)
	for _, user := range usermap {
		if err := srcsender.Send(user); err != nil {
			return fmt.Errorf("error sending users. err: %v", err)
		}
	}
	return nil
}

func (s *Integration) exportPullRequestData(repoids []string) error {
	prsender, err := objsender.NewIncrementalDateBased(s.agent, sourcecode.PullRequestModelName.String())
	if err != nil {
		return err
	}
	prrsender := objsender.NewNotIncremental(s.agent, sourcecode.PullRequestReviewModelName.String())
	prcsender := objsender.NewNotIncremental(s.agent, sourcecode.PullRequestCommentModelName.String())

	defer func() {
		if err := prsender.Done(); err != nil {
			s.logger.Error("error with prsender", "err", err)
		}
		if err := prcsender.Done(); err != nil {
			s.logger.Error("error with prcsender", "err", err)
		}
		if err := prrsender.Done(); err != nil {
			s.logger.Error("error with prrsender", "err", err)
		}
	}()
	incremental := !prsender.LastProcessed.IsZero()
	for _, repoid := range repoids {
		prs, prrs, err := s.api.FetchPullRequests(repoid)
		if err != nil {
			// log error and skip
			s.logger.Error("error fetching pull requests and reviews", "err", err)
			continue
		}
		for _, pr := range prs {
			created := datetime.DateFromEpoch(pr.CreatedDate.Epoch)
			closed := datetime.DateFromEpoch(pr.ClosedDate.Epoch)
			// incremental, only send if this was created after the last processed date
			if !incremental || created.After(prsender.LastProcessed) {
				if err := prsender.Send(pr); err != nil {
					return fmt.Errorf("error sending pull requests. err: %v", err)
				}
			}
			// incremental, only fetch comments if this pr is still opened or was closed after the last processed date
			if !incremental || (pr.Status == sourcecode.PullRequestStatusOpen || (incremental && closed.After(prsender.LastProcessed))) {
				cmts, err := s.api.FetchPullRequestComments(repoid, pr.RefID)
				if err != nil {
					// log error and skip
					s.logger.Error("error fetching pull requests comments", "err", err)
					continue
				}
				for _, prc := range cmts {
					updated := datetime.DateFromEpoch(prc.UpdatedDate.Epoch)
					if !incremental || updated.After(prsender.LastProcessed) {
						if err := prcsender.Send(prc); err != nil {
							return fmt.Errorf("error sending pull requests comments. err: %v", err)
						}
					}
				}
			}
		}
		for _, prr := range prrs {
			if err := prrsender.Send(prr); err != nil {
				return fmt.Errorf("error sending pull request reviews comments. err: %v", err)
			}
		}
	}

	return nil
}

func (s *Integration) onboardExportUsers(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.OnboardExportResult, _ error) {
	repos, projids, err := s.fetcfReposAndProjectIDs()
	if err != nil {
		s.logger.Error("error fetching repos for onboard export users")
		return
	}
	var repoids []string
	for _, repo := range repos {
		repoids = append(repoids, repo.RefID)
	}
	usermap := s.fetchAllUsers(projids, repoids)
	for _, user := range usermap {
		u := agent.UserResponseUsers{
			RefType:    user.RefType,
			RefID:      user.RefID,
			CustomerID: user.CustomerID,
			AvatarURL:  user.AvatarURL,
			Name:       user.Name,
			Username:   *user.Username,
			Active:     true,
		}
		if user.Email != nil {
			u.Emails = []string{*user.Email}
		}
		res.Records = append(res.Records, u.ToMap())
	}
	return res, nil
}

func (s *Integration) onboardExportRepos(ctx context.Context, config rpcdef.ExportConfig) (res rpcdef.OnboardExportResult, err error) {
	repos, _, err := s.fetcfReposAndProjectIDs()
	if err != nil {
		s.logger.Error("error fetching repos for onboard export repos")
		return
	}
	for _, repo := range repos {
		rawcommit, err := s.api.FetchLastCommit(repo.RefID)
		if err != nil {
			s.logger.Error("error fetching last commit for onboard, skipping", "repo ref_id", repo.RefID)
			continue
		}
		r := &agent.RepoResponseRepos{
			Active:      repo.Active,
			Description: repo.Description,
			Language:    repo.Language,
			LastCommit: agent.RepoResponseReposLastCommit{
				Author: agent.RepoResponseReposLastCommitAuthor{
					Name:  rawcommit.Author.Name,
					Email: rawcommit.Author.Email,
				},
				CommitSha: rawcommit.CommitID,
				CommitID:  ids.CodeCommit(s.conf.customerid, s.conf.reftype, repo.ID, rawcommit.CommitID),
				URL:       rawcommit.RemoteURL,
				Message:   rawcommit.Comment,
			},
			Name:    repo.Name,
			RefID:   repo.RefID,
			RefType: repo.RefType,
		}
		if rawcommit.Author.Date != "" {
			if d, err := datetime.ISODateToTime(rawcommit.Author.Date); err != nil {
				s.logger.Error("error converting date in tfs-code onboardExportRepos")
			} else {
				date.ConvertToModel(d, &r.LastCommit.CreatedDate)
			}
		}
		res.Records = append(res.Records, r.ToMap())
	}
	return
}