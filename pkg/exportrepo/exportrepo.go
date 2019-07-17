package exportrepo

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pinpt/go-common/datetime"
	"github.com/pinpt/go-common/fileutil"
	"github.com/pinpt/go-common/hash"

	"github.com/pinpt/go-datamodel/sourcecode"

	"github.com/pinpt/agent.next/pkg/fsconf"
	"github.com/pinpt/agent.next/pkg/gitclone"
	"github.com/pinpt/agent.next/pkg/jsonstore"
	"github.com/pinpt/agent.next/pkg/outsession"
	"github.com/pinpt/ripsrc/ripsrc"

	"github.com/hashicorp/go-hclog"
)

type Opts struct {
	Logger     hclog.Logger
	CustomerID string
	RepoID     string
	Sessions   *outsession.Manager

	LastProcessed *jsonstore.Store
	RepoAccess    gitclone.AccessDetails
}

type Export struct {
	opts   Opts
	locs   fsconf.Locs
	logger hclog.Logger
	//defaultBranch string

	repoNameUsedInCacheDir string
	lastProcessedKey       []string

	rip *ripsrc.Ripsrc
}

func New(opts Opts, locs fsconf.Locs) *Export {
	if opts.CustomerID == "" {
		panic("provide CustomerID")
	}
	if opts.RepoID == "" {
		panic("provide RepoID")
	}
	s := &Export{}
	s.opts = opts
	s.logger = opts.Logger.Named("exportrepo")
	s.locs = locs
	return s
}

func (s *Export) Run(ctx context.Context) error {
	err := os.MkdirAll(s.locs.Temp, 0777)
	if err != nil {
		return err
	}

	checkoutDir, cacheDir, err := s.clone(ctx)
	if err != nil {
		return err
	}

	s.repoNameUsedInCacheDir = filepath.Base(cacheDir)

	s.logger = s.logger.With("repo", s.repoNameUsedInCacheDir)

	s.ripsrcSetup(checkoutDir)

	err = s.branches(ctx)
	if err != nil {
		return err
	}

	err = s.code(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (s *Export) clone(ctx context.Context) (
	checkoutDir string,
	cacheDir string,
	_ error) {

	tempDir, err := ioutil.TempDir(s.locs.Temp, "")
	if err != nil {
		return "", "", err
	}

	dirs := gitclone.Dirs{
		CacheRoot: s.locs.RepoCache,
		Checkout:  tempDir,
	}
	res, err := gitclone.CloneWithCache(ctx, s.logger, s.opts.RepoAccess, dirs)

	if err != nil {
		return "", "", err
	}

	//s.defaultBranch = res.DefaultBranch

	return tempDir, res.CacheDir, nil
}

func (s *Export) ripsrcSetup(repoDir string) {

	opts := ripsrc.Opts{}
	opts.Logger = s.logger.Named("ripsrc")
	opts.RepoDir = repoDir
	opts.AllBranches = true
	opts.BranchesUseOrigin = true
	opts.CheckpointsDir = s.locs.RipsrcCheckpoints

	s.lastProcessedKey = []string{"ripsrc", s.repoNameUsedInCacheDir}

	lastCommit := s.opts.LastProcessed.Get(s.lastProcessedKey...)
	if lastCommit != nil {
		opts.CommitFromIncl = lastCommit.(string)

		if !fileutil.FileExists(opts.CheckpointsDir) {
			panic(fmt.Errorf("expecting to run incrementals, but ripsrc checkpoints dir does not exist for repo: %v", s.repoNameUsedInCacheDir))
		}
	}

	s.logger.Info("setting up ripsrc", "last_processed_old", lastCommit)

	s.rip = ripsrc.New(opts)
}

func (s *Export) branches(ctx context.Context) error {
	sessions := s.opts.Sessions
	sessionID, _, err := sessions.NewSession("sourcecode.branch")
	if err != nil {
		return err
	}
	defer sessions.Done(sessionID, nil)

	res := make(chan ripsrc.Branch)
	done := make(chan bool)

	go func() {
		for data := range res {
			obj := sourcecode.Branch{}
			obj.RefID = ""
			obj.RefType = "git"
			obj.CustomerID = s.opts.CustomerID
			obj.Name = data.Name
			obj.Default = data.IsDefault
			obj.Merged = data.IsMerged
			obj.MergeCommit = data.MergeCommit
			obj.BranchedFromCommits = data.BranchedFromCommits
			obj.Commits = data.Commits
			obj.BehindDefaultCount = int64(data.BehindDefaultCount)
			obj.AheadDefaultCount = int64(data.AheadDefaultCount)
			obj.RepoID = s.opts.RepoID

			err := sessions.Write(sessionID, []map[string]interface{}{
				obj.ToMap(),
			})
			if err != nil {
				panic(err)
			}
		}
		done <- true
	}()

	err = s.rip.Branches(ctx, res)
	<-done

	if err != nil {
		return err
	}

	return nil
}

func (s *Export) code(ctx context.Context) error {
	started := time.Now()

	res := make(chan ripsrc.CommitCode, 100)
	done := make(chan bool)

	lastProcessed := ""
	go func() {
		defer func() { done <- true }()
		var err error
		lastProcessed, err = s.processCode(res)
		if err != nil {
			panic(err)
		}
	}()

	err := s.rip.CodeByCommit(ctx, res)
	if err != nil {
		return err
	}
	<-done

	if lastProcessed != "" {
		err := s.opts.LastProcessed.Set(lastProcessed, s.lastProcessedKey...)
		if err != nil {
			return err
		}
	}

	s.logger.Debug("code processing end", "duration", time.Since(started), "last_processed_new", lastProcessed)

	return nil

}

func (s *Export) processCode(commits chan ripsrc.CommitCode) (lastProcessedSHA string, _ error) {
	sessions := s.opts.Sessions
	blameSession, _, err := sessions.NewSession("sourcecode.blame")
	if err != nil {
		return "", err
	}
	commitSession, _, err := sessions.NewSession("sourcecode.commit")
	if err != nil {
		return "", err
	}

	defer func() {
		sessions.Done(blameSession, nil)
		sessions.Done(commitSession, nil)
	}()

	writeBlame := func(obj sourcecode.Blame) error {
		return sessions.Write(blameSession, []map[string]interface{}{
			obj.ToMap(),
		})
	}
	writeCommit := func(obj sourcecode.Commit) error {
		return sessions.Write(commitSession, []map[string]interface{}{
			obj.ToMap(),
		})
	}

	var commitAdditions int64
	var commitDeletions int64
	var commitCommentsCount int64
	var commitFilesCount int64
	var commitSlocCount int64
	var commitLocCount int64
	var commitBlanksCount int64
	var commitSize int64
	var commitComplexityCount int64

	customerID := s.opts.CustomerID
	refType := "github" //TODO: need to pass from options?
	repoID := s.opts.RepoID
	//urlPrefix := "http://github.com" // TODO: check how to build url below

	for commit := range commits {
		lastProcessedSHA = commit.SHA
		commitAdditions = 0
		commitDeletions = 0
		commitCommentsCount = 0
		commitFilesCount = 0
		commitSlocCount = 0
		commitLocCount = 0
		commitBlanksCount = 0
		commitComplexityCount = 0
		ordinal := datetime.EpochNow()
		var lastBlame *ripsrc.BlameResult
		commitFiles := []sourcecode.CommitFiles{}
		var excludedFilesCount int64
		for blame := range commit.Files {
			lastBlame = &blame
			if blame.Commit.SHA == "" {
				panic(`blame.Commit.SHA == ""`)
			}
			//var license string
			var licenseConfidence float32
			if blame.License != nil {
				//license = fmt.Sprintf("%v (%.0f%%)", blame.License.Name, 100*blame.License.Confidence)
				licenseConfidence = blame.License.Confidence
			}
			//s.logger.Debug(fmt.Sprintf("[%s] %s language=%s,license=%v,loc=%v,sloc=%v,comments=%v,blanks=%v,complexity=%v,skipped=%v", blame.Commit.SHA[0:8], blame.Filename, blame.Language, license, blame.Loc, blame.Sloc, blame.Comments, blame.Comments, blame.Complexity, blame.Skipped))
			lines := []sourcecode.BlameLines{}
			var sloc, loc, comments, blanks int64
			for _, line := range blame.Lines {
				lines = append(lines, sourcecode.BlameLines{
					Sha:         line.SHA,
					AuthorRefID: line.Email,
					Date:        line.Date.Format("2006-01-02T15:04:05.000000Z-07:00"),
					Comment:     line.Comment,
					Code:        line.Code,
					Blank:       line.Blank,
				})
				loc++
				if line.Code {
					sloc++ // safety check below
				}
				if line.Comment {
					comments++
				}
				if line.Blank {
					blanks++
				}
			} // safety check
			if blame.Sloc != sloc {
				panic("logic problem: sloc didn't match")
			}

			commitCommentsCount += comments
			commitSlocCount += sloc
			commitLocCount += loc
			commitBlanksCount += blanks

			cf := blame.Commit.Files[blame.Filename]
			if blame.Language == "" {
				blame.Language = unknownLanguage
			}
			excluded := blame.Skipped != ""

			if excluded {
				excludedFilesCount++
			}
			commitAdditions += int64(cf.Additions)
			commitDeletions += int64(cf.Deletions)
			var lic string
			if blame.License != nil {
				lic = blame.License.Name
			}

			commitFiles = append(commitFiles, sourcecode.CommitFiles{
				CommitID:          hash.Values("Commit", customerID, refType, commit.SHA),
				RepoID:            hash.Values("Repo", customerID, refType, repoID),
				Status:            string(cf.Status),
				Ordinal:           ordinal,
				Created:           timeCommitCreated(blame.Commit.Date),
				Filename:          cf.Filename,
				Language:          blame.Language,
				Renamed:           cf.Renamed,
				RenamedFrom:       cf.RenamedFrom,
				RenamedTo:         cf.RenamedTo,
				Additions:         int64(cf.Additions),
				Deletions:         int64(cf.Deletions),
				Binary:            cf.Binary,
				Excluded:          blame.Skipped != "",
				ExcludedReason:    blame.Skipped,
				License:           lic,
				LicenseConfidence: float64(licenseConfidence),
				Size:              blame.Size,
				Loc:               blame.Loc,
				Sloc:              blame.Sloc,
				Comments:          blame.Comments,
				Blanks:            blame.Blanks,
				Complexity:        blame.Complexity,
			})

			commitComplexityCount += blame.Complexity
			commitSize += blame.Size
			commitFilesCount++
			// if exclude but not deleted, we don't need to write to commit activity
			// we need to write to commit_activity for deleted so we can track the last
			// deleted commit so sloc will add correctly to reflect the deleted sloc
			if excluded && cf.Status != ripsrc.GitFileCommitStatusRemoved {
				continue
			}

			err := writeBlame(sourcecode.Blame{
				Status:         statusFromRipsrc(blame.Status),
				License:        &lic,
				Excluded:       blame.Skipped != "",
				ExcludedReason: blame.Skipped,
				CommitID:       hash.Values("Commit", customerID, refType, blame.Commit.SHA),
				RefID:          blame.Commit.SHA,
				RefType:        refType,
				CustomerID:     customerID,
				Hashcode:       "",
				Size:           blame.Size,
				Loc:            int64(loc),
				Sloc:           int64(sloc),
				Blanks:         int64(blanks),
				Comments:       int64(comments),
				Filename:       blame.Filename,
				Language:       blame.Language,
				Sha:            blame.Commit.SHA,
				Date:           timeBlameDate(blame.Commit.Date),
				RepoID:         hash.Values("Repo", customerID, refType, repoID),
				Complexity:     blame.Complexity,
				Lines:          lines,
			})
			if err != nil {
				return "", err
			}
			ordinal++
		}

		if lastBlame != nil {
			err := writeCommit(sourcecode.Commit{
				RefID:      commit.SHA,
				RefType:    refType,
				CustomerID: customerID,
				Hashcode:   "",
				RepoID:     hash.Values("Repo", customerID, refType, repoID),
				Sha:        commit.SHA,
				Message:    lastBlame.Commit.Message,
				//URL:            buildURL(refType, getHtmlURLPrefix(urlPrefix), repoName, commit.SHA), //TODO: i don't have access to reponame
				Created: timeCommitCreated(lastBlame.Commit.Date),
				//Branch:         branch, // TODO: this field is not correct at all
				Additions:      commitAdditions,
				Deletions:      commitDeletions,
				FilesChanged:   commitFilesCount,
				AuthorRefID:    hash.Values(customerID, lastBlame.Commit.AuthorEmail),
				CommitterRefID: hash.Values(customerID, lastBlame.Commit.CommitterEmail),
				Ordinal:        lastBlame.Commit.Ordinal,
				Loc:            commitLocCount,
				Sloc:           commitSlocCount,
				Comments:       commitCommentsCount,
				Blanks:         commitBlanksCount,
				Size:           commitSize,
				Complexity:     commitComplexityCount,
				GpgSigned:      lastBlame.Commit.Signed,
				Excluded:       excludedFilesCount == commitFilesCount,
				Files:          commitFiles,
			})
			if err != nil {
				return "", err
			}
		}

	}

	return
}

const (
	unknownUser     = "unknown-deleter"
	unknownLanguage = "unknown"
)

func statusFromRipsrc(status ripsrc.CommitStatus) sourcecode.BlameStatus {
	switch status {
	case ripsrc.GitFileCommitStatusAdded:
		return sourcecode.BlameStatusAdded
	case ripsrc.GitFileCommitStatusModified:
		return sourcecode.BlameStatusModified
	case ripsrc.GitFileCommitStatusRemoved:
		return sourcecode.BlameStatusRemoved
	}
	return 0
}

func buildURL(refType string, prefixURL string, repoFullName string, sha string) string {
	if refType == "bitbucket" {
		return fmt.Sprintf("%s/%s/commits/%s", prefixURL, repoFullName, sha)
	} else {
		return fmt.Sprintf("%s/%s/commit/%s", prefixURL, repoFullName, sha)
	}
}

func getHtmlURLPrefix(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		panic(err)
	}

	words := strings.Split(u.Host, ".")

	return u.Scheme + "://" + strings.Join(words[len(words)-2:], ".")
}
