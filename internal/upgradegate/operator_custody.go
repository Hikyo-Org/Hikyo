package upgradegate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

const operatorCustodyName = "operator-custody.json"

// The installation pin and epoch floor live outside the replaceable database.
// An unfinished journal blocks all admission until the same local operation resumes.
type operatorCustody struct {
	Format     string           `json:"format"`
	InstanceID string           `json:"instance_id"`
	PublicKey  []byte           `json:"public_key"`
	EpochFloor int64            `json:"epoch_floor"`
	Journal    *operatorJournal `json:"journal"`
}
type operatorJournal struct {
	Digest    releaseidentity.Digest `json:"digest"`
	Before    upgrade.State          `json:"before"`
	After     upgrade.State          `json:"after"`
	PublicKey []byte                 `json:"public_key"`
}

func decodeOperatorCustody(raw []byte) (operatorCustody, error) {
	var c operatorCustody
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&c); err != nil {
		return c, err
	}
	if err := d.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return c, errors.New("invalid operator custody trailer")
	}
	canonical, err := json.Marshal(c)
	if err != nil || !bytes.Equal(canonical, raw) || c.Format != "hikyo-operator-custody/v1" || c.EpochFloor < 0 {
		return c, errors.New("invalid operator custody")
	}
	instance := c.InstanceID
	if instance == "" {
		instance = "ins_00000000000000000000000000000000"
	}
	if _, err := backupreceipt.PinOperator(instance, c.PublicKey); err != nil {
		return c, err
	}
	if c.Journal != nil {
		j := c.Journal
		next, err := backupreceipt.PinOperator(c.InstanceID, j.PublicKey)
		prior, _ := backupreceipt.PinOperator(c.InstanceID, c.PublicKey)
		if err != nil || next.KeyID() == prior.KeyID() || j.Digest.Validate() != nil || j.Before.Validate() != nil || j.After.Validate() != nil || j.Before.InstanceID != c.InstanceID || j.After.InstanceID != c.InstanceID || j.After.RestoreEpoch <= j.Before.RestoreEpoch || !j.After.Pending.Invalidated {
			return c, errors.New("invalid operator rotation journal")
		}
	}
	return c, nil
}
func (c operatorCustody) pin(instance string) (backupreceipt.PinnedOperator, error) {
	if c.Journal != nil {
		return backupreceipt.PinnedOperator{}, errors.New("operator rotation incomplete; resume the exact local operator command")
	}
	if c.InstanceID != "" && c.InstanceID != instance {
		return backupreceipt.PinnedOperator{}, errors.New("operator custody belongs to a different installation")
	}
	return backupreceipt.PinOperator(instance, c.PublicKey)
}
func (c operatorCustody) check(state upgrade.State) error {
	pin, err := c.pin(state.InstanceID)
	if err != nil {
		return err
	}
	if state.RestoreEpoch < c.EpochFloor {
		return errors.New("database predates the durable operator epoch floor; explicit recovery required")
	}
	if state.Pending != nil && !state.Pending.Invalidated && state.Pending.Acceptance.Attestation != nil && state.Pending.Acceptance.Attestation.OperatorKeyID != pin.KeyID() {
		return errors.New("operator key differs from durable installation pin")
	}
	return nil
}
