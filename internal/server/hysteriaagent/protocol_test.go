package hysteriaagent

import (
	"context"
	"testing"

	commonproto "laz/generated/nodeproto/agentcommon"
	"laz/internal/server/agentprotocol"
)

func TestBuildHysteriaCredentialChangeUsesStoredPasswordHash(t *testing.T) {
	const passwordHash = "$2a$10$stable-password-hash"
	change, err := buildHysteriaCredentialChange(context.Background(), agentprotocol.StoredConnection[string]{
		ConnectionID: "connection-1",
		Active:       true,
		Material:     passwordHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if change.GetConnectionId() != "connection-1" || change.GetOp() != commonproto.CredentialChangeOp_CREDENTIAL_CHANGE_OP_UPSERT {
		t.Fatalf("unexpected credential change: %+v", change)
	}
	if change.GetPayload().GetPasswordHash() != passwordHash {
		t.Fatalf("manifest changed stored password hash: got %q want %q", change.GetPayload().GetPasswordHash(), passwordHash)
	}
}
