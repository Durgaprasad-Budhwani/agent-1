package e2etests

import (
	"context"
	"reflect"
	"testing"

	"github.com/pinpt/agent/slimrippy/internal/branchmeta"
	"github.com/pinpt/agent/slimrippy/testutil"
)

type Test struct {
	t              *testing.T
	repoName       string
	includeDefault bool
}

func NewTest(t *testing.T, repoName string, includeDefault bool) *Test {
	s := &Test{}
	s.t = t
	s.repoName = repoName
	s.includeDefault = includeDefault
	return s
}

func (s *Test) Run() []branchmeta.Branch {
	t := s.t
	dirs := testutil.UnzipTestRepo(s.repoName)
	defer dirs.Remove()

	ctx := context.Background()
	res, err := branchmeta.GetAll(ctx, dirs.RepoDir, s.includeDefault)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func assertResult(t *testing.T, want, got []branchmeta.Branch) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("invalid result count, wanted %v, got %v", len(want), len(got))
	}
	gotCopy := make([]branchmeta.Branch, len(got))
	copy(gotCopy, got)

	for i := range want {
		g := gotCopy[i]
		if !reflect.DeepEqual(want[i], g) {
			t.Fatalf("invalid branch, wanted\n%+v\ngot\n%+v", want[i], got[i])
		}
	}
}
