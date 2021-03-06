package api

import (
	"time"

	"github.com/pinpt/agent/pkg/ids2"
)

// used in changelogResponse struct and work_changelog.go
type changelogField struct {
	NewValue interface{} `json:"newValue"`
	OldValue interface{} `json:"oldvalue"`
}

type changelogFieldWithIDGen struct {
	changelogField
	gen ids2.Gen
}

// used in work_changelog.go - fetchChangeLog
type changelogResponse struct {
	Fields      map[string]changelogField `json:"fields"`
	ID          int64                     `json:"id"`
	RevisedDate time.Time                 `json:"revisedDate"`
	URL         string                    `json:"url"`
	Relations   struct {
		Added []struct {
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
			URL string `json:"url"`
		} `json:"added"`
		Removed []struct {
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
			URL string `json:"url"`
		} `json:"removed"`
	} `json:"relations"`
	RevisedBy usersResponse `json:"revisedBy"`
}

// type workItemOperation struct {
// 	Op    string  `json:"op"`
// 	Path  string  `json:"path"`
// 	From  *string `json:"from"`
// 	Value string  `json:"value"`
// }

// used in work_item.go - fetchItemIDs
type workItemsResponse struct {
	AsOf    time.Time `json:"asOf"`
	Columns []struct {
		Name          string `json:"name"`
		ReferenceName string `json:"referenceName"`
		URL           string `json:"url"`
	} `json:"columns"`
	QueryResultType string `json:"queryResultType"`
	QueryType       string `json:"queryType"`
	SortColumns     []struct {
		Descending bool `json:"descending"`
		Field      struct {
			Name          string `json:"name"`
			ReferenceName string `json:"referenceName"`
			URL           string `json:"url"`
		} `json:"field"`
	} `json:"sortColumns"`
	WorkItems []struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	} `json:"workItems"`
}

// used in work_item.go - FetchWorkItems
type WorkItemResponse struct {
	Links struct {
		HTML struct {
			HREF string `json:"href"`
		} `json:"html"`
		// there are more here, fields, self, workItemComments, workItemRevisions, workItemType, and workItemUpdates
	} `json:"_links"`
	Fields struct {
		AssignedTo     usersResponse `json:"System.AssignedTo"`
		ChangedDate    time.Time     `json:"System.ChangedDate"`
		CreatedDate    time.Time     `json:"System.CreatedDate"`
		CreatedBy      usersResponse `json:"System.CreatedBy"`
		Description    string        `json:"System.Description"`
		DueDate        time.Time     `json:"Microsoft.VSTS.Scheduling.DueDate"` // ??
		IterationPath  string        `json:"System.IterationPath"`
		TeamProject    string        `json:"System.TeamProject"`
		Priority       int           `json:"Microsoft.VSTS.Common.Priority"`
		Reason         string        `json:"System.Reason"`
		ResolvedReason string        `json:"Microsoft.VSTS.Common.ResolvedReason"`
		ResolvedDate   time.Time     `json:"Microsoft.VSTS.Common.ResolvedDate"`
		StoryPoints    float64       `json:"Microsoft.VSTS.Scheduling.StoryPoints"`
		State          string        `json:"System.State"`
		Tags           string        `json:"System.Tags"`
		Title          string        `json:"System.Title"`
		WorkItemType   string        `json:"System.WorkItemType"`
	} `json:"fields"`
	Relations []struct {
		Rel string `json:"rel"`
		URL string `json:"url"`
	} `json:"relations"`
	ID  int    `json:"id"`
	URL string `json:"url"`
}

// used in work_sprints.go - fetchSprint
type sprintsResponse struct {
	Attributes struct {
		FinishDate time.Time `json:"finishDate"`
		StartDate  time.Time `json:"startDate"`
		TimeFrame  string    `json:"timeFrame"` // past, current, future
	} `json:"attributes"`
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url"`
}
