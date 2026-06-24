package reconcile

import (
	"context"
	amneziaproto "laz/generated/nodeproto/amnezia"
	"laz/internal/legacy/agent/amneziawg/tools"
	"strings"
	"testing"
)

type fakeTools struct {
	dump tools.InterfaceDump
	err  map[string]error
	ops  []string
}

func (f *fakeTools) ShowDump(context.Context) (tools.InterfaceDump, error) {
	return f.dump, f.err["show-dump"]
}
func (f *fakeTools) SetPeer(_ context.Context, publicKey string, opts tools.PeerOptions) error {
	f.ops = append(f.ops, "set-peer:"+publicKey+":"+strings.Join(opts.AllowedIPs, ","))
	return f.err["set-peer:"+publicKey]
}
func (f *fakeTools) SetPeerAllowedIPs(_ context.Context, publicKey string, allowedIPs []string) error {
	f.ops = append(f.ops, "set-allowed:"+publicKey+":"+strings.Join(allowedIPs, ","))
	return f.err["set-allowed:"+publicKey]
}
func (f *fakeTools) RemovePeer(_ context.Context, publicKey string) error {
	f.ops = append(f.ops, "remove-peer:"+publicKey)
	return f.err["remove-peer:"+publicKey]
}

func (f *fakeTools) SetInterfaceConfig(_ context.Context, config tools.InterfaceConfig) error {
	f.ops = append(f.ops, "set-interface")
	return f.err["set-interface"]
}

func TestEditServerConfigAppliesMutableAmneziaWGFields(t *testing.T) {
	ft := &fakeTools{err: map[string]error{}}
	command := &amneziaproto.EditServerConfig{NodeId: "node-1", Revision: 77, ServerConfig: validServerConfig()}

	result := New(ft).EditServerConfig(context.Background(), command)
	if result.GetStatus() != "applied" || result.GetAppliedRevision() != 77 || result.GetNodeId() != "node-1" {
		t.Fatalf("result = %+v", result)
	}
	assertOps(t, ft.ops, []string{"set-interface"})
}

func TestEditServerConfigRejectsMissingContentBeforeTools(t *testing.T) {
	ft := &fakeTools{err: map[string]error{}}
	result := New(ft).EditServerConfig(context.Background(), &amneziaproto.EditServerConfig{NodeId: "node-1", Revision: 77})
	if result.GetStatus() != "rejected" || !strings.Contains(result.GetError(), "server_config") {
		t.Fatalf("result = %+v", result)
	}
	if len(ft.ops) != 0 {
		t.Fatalf("tools called for invalid config: %v", ft.ops)
	}
}

func TestEditServerConfigRejectsInvalidMutableFieldsBeforeTools(t *testing.T) {
	zeroRequired := validServerConfig()
	zeroRequired.H1 = "invalid"
	badRange := validServerConfig()
	badRange.Jmin, badRange.Jmax = 4, 3
	outOfRange := validServerConfig()
	outOfRange.S1 = 70000
	for _, tt := range []struct {
		name    string
		content *amneziaproto.ServerConfig
		wantErr string
	}{
		{name: "invalid header", content: zeroRequired, wantErr: "H1"},
		{name: "jmin greater than jmax", content: badRange, wantErr: "jmin"},
		{name: "out of range", content: outOfRange, wantErr: "s1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ft := &fakeTools{err: map[string]error{}}
			result := New(ft).EditServerConfig(context.Background(), &amneziaproto.EditServerConfig{NodeId: "node-1", Revision: 77, ServerConfig: tt.content})
			if result.GetStatus() != "rejected" || !strings.Contains(result.GetError(), tt.wantErr) {
				t.Fatalf("result = %+v", result)
			}
			if len(ft.ops) != 0 {
				t.Fatalf("tools called for invalid config: %v", ft.ops)
			}
		})
	}
}

func validServerConfig() *amneziaproto.ServerConfig {
	return &amneziaproto.ServerConfig{Jc: 1, Jmin: 2, Jmax: 3, S1: 4, S2: 5, S3: 6, S4: 7, H1: "8", H2: "9", H3: "10", H4: "11", I1: "<r 12>", I2: "<r 13>", I3: "<r 14>", I4: "<r 15>", I5: "<r 16>", ServerPrivateKey: "server-private", ServerIpv4: "10.8.1.1", ClientIpv4Cidr: "10.8.1.0/24", ListenPort: 51820}
}

func assertOps(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ops = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ops = %#v, want %#v", got, want)
		}
	}
}
