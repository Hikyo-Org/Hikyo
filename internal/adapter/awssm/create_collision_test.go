package awssm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

type createCollisionAPI struct {
	*fakeAPI
	firstDescribe bool
}

func (a *createCollisionAPI) DescribeSecret(ctx context.Context, name string) (SecretMetadata, error) {
	if !a.firstDescribe {
		a.firstDescribe = true
		return SecretMetadata{}, &ResponseError{Status: 400, Code: "ResourceNotFoundException"}
	}
	return a.fakeAPI.DescribeSecret(ctx, name)
}

func TestCreateCollisionRechecksCurrentVersion(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(map[bool]string{false: "external version refused", true: "explicit consent accepted"}[approved], func(t *testing.T) {
			api := newFakeAPI()
			tags := map[string]string{adapter.SentinelName: testTarget}
			if approved {
				tags[VersionTag] = "external"
			}
			api.secrets["prod/app"] = &fakeSecret{tags: tags, stages: map[string][]string{"external": {awsCurrent}}, values: map[string]string{"external": "do not overwrite"}}
			journal := newFakeJournal()
			_, err := (&Module{API: &createCollisionAPI{fakeAPI: api}}).Sync(t.Context(), adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "job_collision"}, journal)
			if approved {
				if err != nil || api.current("prod/app") == "do not overwrite" {
					t.Fatalf("consented collision: %v", err)
				}
				return
			}
			if !errors.Is(err, adapter.ErrConflict) {
				t.Fatalf("unapproved collision: %v", err)
			}
			if api.current("prod/app") != "do not overwrite" {
				t.Fatal("external version overwritten")
			}
			for _, call := range api.writes() {
				if !strings.HasPrefix(call, "create:") {
					t.Fatalf("collision dispatched mutation: %s", call)
				}
			}
		})
	}
}
