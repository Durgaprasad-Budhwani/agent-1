package azurecommon

import (
	"net/url"
	"strings"
	"time"

	"github.com/pinpt/agent.next/pkg/commitusers"
	"github.com/pinpt/agent.next/pkg/objsender"
	"github.com/pinpt/agent.next/rpcdef"
	pjson "github.com/pinpt/go-common/json"
	"github.com/pinpt/integration-sdk/sourcecode"
)

func (s *Integration) exportCode() error {
	repoids, projectids, err := s.processRepos()
	if err != nil {
		return err
	}
	if err := s.processPullRequests(repoids); err != nil {
		return err
	}
	if err := s.processUsers(repoids, projectids); err != nil {
		return err
	}
	return nil
}

func urlAppend(p1, p2 string) string {
	return strings.TrimSuffix(p1, "/") + "/" + p2
}

func commitURLTemplate(reponame, repoURLPrefix string) string {
	return urlAppend(repoURLPrefix, reponame) + "/commit/@@@sha@@@"
}

func branchURLTemplate(reponame, repoURLPrefix string) string {
	return urlAppend(repoURLPrefix, reponame) + "/tree/@@@branch@@@"
}

func stringify(i interface{}) string {
	return pjson.Stringify(i)
}

func (s *Integration) processRepos() (repoids []string, projectids []string, err error) {
	sender := objsender.NewNotIncremental(s.agent, sourcecode.RepoModelName.String())
	defer sender.Done()

	var repos []*sourcecode.Repo
	if repos, projectids, err = s.api.FetchAllRepos(s.IncludedRepos, s.ExcludedRepoIDs); err != nil {
		return
	}
	for _, repo := range repos {
		repoids = append(repoids, repo.RefID)
		if err := sender.Send(repo); err != nil {
			s.logger.Error("error sending repo", "data", repo.Stringify())
		}
		if err := s.ripSource(repo); err != nil {
			s.logger.Error("error with ripsrc in repo", "data", repo.Stringify())
		}
	}
	return
}

func (s *Integration) ripSource(repo *sourcecode.Repo) error {
	u, err := url.Parse(repo.URL)
	if s.OverrideGitHostName != "" {
		u.Host = s.OverrideGitHostName
	}
	if err != nil {
		return err
	}
	u.User = url.UserPassword(s.creds.Username, s.creds.Password)
	args := rpcdef.GitRepoFetch{}
	args.RefType = s.reftype.String()
	args.RepoID = s.api.RepoID(repo.RefID)
	args.URL = u.String()
	s.logger.Info("cloning repo " + u.String())
	args.BranchURLTemplate = branchURLTemplate(repo.Name, s.creds.URL)
	args.CommitURLTemplate = commitURLTemplate(repo.Name, s.creds.URL)
	return s.agent.ExportGitRepo(args)
}

func (s *Integration) processPullRequests(repoids []string) error {
	senderprs, err := objsender.NewIncrementalDateBased(s.agent, sourcecode.PullRequestModelName.String())
	if err != nil {
		return err
	}
	defer senderprs.Done()
	senderprrs, err := objsender.NewIncrementalDateBased(s.agent, sourcecode.PullRequestReviewModelName.String())
	if err != nil {
		return err
	}
	defer senderprrs.Done()
	senderprcs, err := objsender.NewIncrementalDateBased(s.agent, sourcecode.PullRequestCommentModelName.String())
	if err != nil {
		return err
	}
	defer senderprcs.Done()
	prchan, prdone := s.execute("pull requests", senderprs)
	prrchan, prrdone := s.execute("pull request reviews", senderprrs)
	prcchan, prcdone := s.execute("pull request comments", senderprcs)
	if err := s.api.FetchPullRequests(repoids, senderprs.LastProcessed, prchan, prrchan, prcchan); err != nil {
		return err
	}
	close(prchan)
	close(prrchan)
	close(prcchan)
	<-prdone
	<-prrdone
	<-prcdone
	return nil
}

func (s *Integration) processUsers(repoids []string, projectids []string) error {

	// not sure if we should call the incremental sender in commit users,
	// this is the only api with incremental, but we're only fething users to match the other user api
	sendercomm := objsender.NewNotIncremental(s.agent, commitusers.TableName)
	senderproj := objsender.NewNotIncremental(s.agent, sourcecode.UserModelName.String())
	defer func() {
		if err := senderproj.Done(); err != nil {
			s.logger.Error("error closing project user sender, senderproj.Done()")
		}
		if err := sendercomm.Done(); err != nil {
			s.logger.Error("error closing commit user sender, sendercomm.Done()")
		}
	}()

	projusers, err := s.api.FetchSourcecodeUsers(projectids)
	if err != nil {
		return err
	}
	commusers, err := s.api.FetchCommitUsers(repoids, time.Time{} /* sendercomm.LastProcessed */)
	if err != nil {
		return err
	}

	// Commit Users:
	// only send the commit users who's email matches the UniqueName of a project user
	// the key of the commit user map is the email
	// the key of the project user map is the UniqueName, which is usually the user's email
	for email, commitusr := range commusers {
		if u, o := projusers[email]; o {
			commitusr.SourceID = u.RefID
			if err := commitusr.Validate(); err != nil {
				s.logger.Error("error validating commit user, skipping", "data", commitusr.Stringify())
				continue
			}
			s.logger.Info("sending commit user", "data", commitusr.Stringify())
			if err := sendercomm.SendMap(commitusr.ToMap()); err != nil {
				s.logger.Error("error sending commit user", "data", commitusr.Stringify())
			}
		}
	}

	// Project Users:
	for _, user := range projusers {
		if err := senderproj.Send(user); err != nil {
			s.logger.Error("error sending project user", "data", user.Stringify())
		}
	}
	return nil
}
