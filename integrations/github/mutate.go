package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pinpt/agent/integrations/github/api"
	"github.com/pinpt/agent/integrations/pkg/mutate"
	"github.com/pinpt/agent/rpcdef"
	"github.com/pinpt/go-common/datamodel"
	"github.com/pinpt/integration-sdk/agent"
	"github.com/pinpt/integration-sdk/sourcecode"
)

func (s *Integration) returnUpdatedPR(prRefID string) (res rpcdef.MutateResult, rerr error) {
	pr, err := api.PullRequestByID(s.qc, prRefID)
	if err != nil {
		rerr = err
		return
	}
	m := pr.ToMap()
	delete(m, "created_by_ref_id")
	delete(m, "closed_by_ref_id")
	delete(m, "merged_by_ref_id")
	delete(m, "commit_ids")
	delete(m, "commit_shas")
	objs := rpcdef.MutatedObjects{}
	objs[sourcecode.PullRequestModelName.String()] = []interface{}{m}
	res.MutatedObjects = objs
	return
}

type Model interface {
	ToMap() map[string]interface{}
}

func (s *Integration) mutationResult(modelName datamodel.ModelNameType, obj Model) (res rpcdef.MutateResult, rerr error) {
	objs := rpcdef.MutatedObjects{}
	objs[modelName.String()] = []interface{}{obj.ToMap()}
	res.MutatedObjects = objs
	return
}

func (s *Integration) Mutate(ctx context.Context, fn, data string, config rpcdef.ExportConfig) (res rpcdef.MutateResult, _ error) {

	rerr := func(err error) {
		res = mutate.ResultFromError(err)
	}

	err := s.initWithConfig(config)
	if err != nil {
		rerr(err)
		return
	}

	s.qc.Request = s.makeRequestNoRetries

	action := mutate.UnmarshalAction(fn)

	switch action {
	// this is actually pr title
	case agent.IntegrationMutationRequestActionIssueSetTitle:
		var obj struct {
			RefID string `json:"ref_id"`
			Title string `json:"title"`
		}
		err := json.Unmarshal([]byte(data), &obj)
		if err != nil {
			rerr(err)
			return
		}
		err = api.PREditTitle(s.qc, obj.RefID, obj.Title)
		if err != nil {
			rerr(err)
			return
		}
		return s.returnUpdatedPR(obj.RefID)
	}

	rerr(fmt.Errorf("mutate fn not supported: %v", fn))
	return
}
