package api

import (
	pjson "github.com/pinpt/go-common/json"
)

type CommitAuthor struct {
	CommitHash     string
	AuthorName     string
	AuthorEmail    string
	AuthorRefID    string
	CommitterName  string
	CommitterEmail string
	CommitterRefID string
}

func CommitsPage(
	qc QueryContext,
	repo Repo, branchName string,
	queryParams string) (pi PageInfo, res []CommitAuthor, _ error) {

	qc.Logger.Debug("commits request", "repo", repo.NameWithOwner, "branchName", branchName, "q", queryParams)

	query := `
	query {
		node (id: "` + repo.ID + `") {
			... on Repository {
				ref(qualifiedName: ` + pjson.Stringify(branchName) + `){
					target {
						... on Commit {
							history(` + queryParams + `){
								totalCount
								pageInfo {
									hasNextPage
									endCursor
									hasPreviousPage
									startCursor
								}
								nodes {
									oid
									author {
										name
										email
										user {
											login
										}
									}
									committer {
										name
										email
										user {
											login
										}
									}
								}	
							}
						}
					}
				}
			}
		}
	}
	`

	var requestRes struct {
		Data struct {
			Node struct {
				Ref struct {
					Target struct {
						History struct {
							TotalCount int      `json:"totalCount"`
							PageInfo   PageInfo `json:"pageInfo"`
							Nodes      []struct {
								OID    string `json:"oid"`
								Author struct {
									Name  string `json:"name"`
									Email string `json:"email"`
									User  struct {
										Login string `json:"login"`
									} `json:"user"`
								} `json:"author"`
								Committer struct {
									Name  string `json:"name"`
									Email string `json:"email"`
									User  struct {
										Login string `json:"login"`
									} `json:"user"`
								} `json:"committer"`
							} `json:"nodes"`
						} `json:"history"`
					} `json:"target"`
				} `json:"ref"`
			} `json:"node"`
		} `json:"data"`
	}

	err := qc.Request(query, &requestRes)
	if err != nil {
		return pi, res, err
	}

	//qc.Logger.Info(fmt.Sprintf("object %+v", requestRes))

	commits := requestRes.Data.Node.Ref.Target.History
	commitNodes := commits.Nodes

	for _, data := range commitNodes {
		item := CommitAuthor{}
		item.CommitHash = data.OID

		if data.Author.User.Login != "" {
			login := data.Author.User.Login
			email := data.Author.Email
			item.AuthorRefID, err = qc.UserLoginToRefIDFromCommit(login, email)
			if err != nil {
				qc.Logger.Error("could not resolve author when processing commit", "login", login, "repo", repo.NameWithOwner, "commit", item.CommitHash)
			}
		}
		if data.Committer.User.Login != "" {
			login := data.Committer.User.Login
			email := data.Committer.Email
			item.CommitterRefID, err = qc.UserLoginToRefIDFromCommit(login, email)
			if err != nil {
				qc.Logger.Error("could not resolve committer when processing commit", "login", login, "repo", repo.NameWithOwner, "commit", item.CommitHash)
			}
		}
		item.AuthorName = data.Author.Name
		item.AuthorEmail = data.Author.Email
		item.CommitterName = data.Committer.Name
		item.CommitterEmail = data.Committer.Email

		res = append(res, item)
	}

	return commits.PageInfo, res, nil
}
