package isolation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestSCIMAuditBlindsPredictableIdentity(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx, svc := t.Context(), scimSvc(db)
		binding, credential := newSCIMBinding(t, db, "okta")
		actor := service.SCIMCredentialActor(credential, binding)
		const subject = "employee-123"
		create := func() service.DesiredUser {
			return service.DesiredUser{Active: true, UserName: subject, ExternalID: subject, SubjectRaw: subject}
		}
		first, err := svc.CreateUser(ctx, actor, orgA, binding, create())
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.DeleteUser(ctx, actor, orgA, binding, first.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CreateUser(ctx, actor, orgA, binding, create()); err != nil {
			t.Fatal(err)
		}
		raw := queryStrings(t, db, `SELECT payload FROM audit_tenant_events WHERE type = 'scim.user_provisioned' ORDER BY id`)
		dictionary := sha256.Sum256([]byte(subject))
		var commitments []string
		decoder := json.NewDecoder(strings.NewReader(raw))
		for {
			var payload map[string]any
			if err := decoder.Decode(&payload); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			digest, ok := payload["subject_digest"].(string)
			decoded, err := hex.DecodeString(digest)
			if !ok || err != nil || len(decoded) != sha256.Size || digest == hex.EncodeToString(dictionary[:]) {
				t.Fatal("audit subject leaked a dictionary hash or changed v1 format")
			}
			commitments = append(commitments, digest)
		}
		if len(commitments) != 2 || commitments[0] == commitments[1] {
			t.Fatalf("same-subject events leak equality: %v", commitments)
		}
	})
}
