package isolation

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
)

// These fault injectors leave the real SQL runtime/journal in charge of all
// custody, intent, lease, crash reconciliation and outcome transitions.
type awsPromotedAdoptionClient struct {
	*awssm.Client
	afterPromotion func(context.Context) error
}

func (c *awsPromotedAdoptionClient) UpdateSecretVersionStage(ctx context.Context, id, stage, moveTo, removeFrom string) error {
	if err := c.Client.UpdateSecretVersionStage(ctx, id, stage, moveTo, removeFrom); err != nil {
		return err
	}
	if stage == "AWSCURRENT" {
		return c.afterPromotion(ctx)
	}
	return nil
}

type awsAdoptionFinishModule struct {
	adapter.Module
	finish func(string) error
}

func (m *awsAdoptionFinishModule) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	return m.Module.Sync(ctx, req, awsAdoptionFinishJournal{Journal: journal, finish: func() error { return m.finish(req.Target.ID) }})
}

type awsAdoptionFinishJournal struct {
	adapter.Journal
	finish func() error
}

func (j awsAdoptionFinishJournal) Finish(ctx context.Context, effect adapter.Effect, completion adapter.Completion) error {
	if completion.Outcome == adapter.OutcomeSuccess {
		if err := j.finish(); err != nil {
			return err
		}
	}
	return j.Journal.Finish(ctx, effect, completion)
}

type awsAdoptionReplayLoader struct {
	adapter.Loader
	t                        *testing.T
	targetID, jobID, witness string
	loads                    int
}

func (l *awsAdoptionReplayLoader) Load(ctx context.Context, job adapter.Job, journal adapter.Journal) (adapter.LoadedSync, error) {
	loaded, err := l.Loader.Load(ctx, job, journal)
	if err != nil || job.TargetID != l.targetID {
		return loaded, err
	}
	l.loads++
	if len(loaded.Request.Ledger) != 1 {
		l.t.Fatal("durable adoption ledger missing")
	}
	entry := loaded.Request.Ledger[0]
	if entry.State != adapter.Owned || !entry.AdoptionPending || entry.AdoptionWitness == "" {
		l.t.Fatal("retry lost pending Owned adoption or its witness")
	}
	if l.loads == 1 {
		l.jobID, l.witness = job.ID, entry.AdoptionWitness
	} else if job.ID != l.jobID || entry.AdoptionWitness != l.witness {
		l.t.Fatal("retry changed the durable job or plan-time consent witness")
	}
	return loaded, nil
}
