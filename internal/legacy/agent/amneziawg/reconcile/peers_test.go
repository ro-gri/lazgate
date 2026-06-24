package reconcile

import (
	"context"
	"errors"
	amneziaproto "laz/generated/nodeproto/amnezia"
	"laz/internal/legacy/agent/amneziawg/tools"
	"strings"
	"testing"
)

func TestUpsertPeer(t *testing.T) {
	for _, tc := range []struct{ status, ip, want string }{{"active", "10.8.1.2", "10.8.1.2/32"}, {"held", "10.8.1.2", SentinelAllowedIP}} {
		ft := &fakeTools{}
		err := New(ft).UpsertPeer(context.Background(), &amneziaproto.Credential{PublicKey: "peer", DesiredStatus: tc.status, AssignedIpv4: tc.ip})
		if err != nil {
			t.Fatal(err)
		}
		assertOps(t, ft.ops, []string{"set-peer:peer:" + tc.want})
	}
}
func TestRemovePeer(t *testing.T) {
	ft := &fakeTools{}
	if err := New(ft).RemovePeer(context.Background(), "peer"); err != nil {
		t.Fatal(err)
	}
	assertOps(t, ft.ops, []string{"remove-peer:peer"})
}
func TestPruneRuntimePeers(t *testing.T) {
	ft := &fakeTools{dump: tools.InterfaceDump{Peers: []tools.PeerDump{{PublicKey: "keep"}, {PublicKey: "orphan"}}}}
	if err := New(ft).PruneRuntimePeers(context.Background(), map[string]bool{"keep": true}); err != nil {
		t.Fatal(err)
	}
	assertOps(t, ft.ops, []string{"remove-peer:orphan"})
}
func TestInvalidPeerRejectedBeforeTools(t *testing.T) {
	for _, credential := range []*amneziaproto.Credential{nil, {PublicKey: "peer", DesiredStatus: "active", AssignedIpv4: "fd00::2"}, {DesiredStatus: "active", AssignedIpv4: "10.8.1.2"}} {
		ft := &fakeTools{}
		if err := New(ft).UpsertPeer(context.Background(), credential); err == nil {
			t.Fatal("invalid peer accepted")
		}
		if len(ft.ops) != 0 {
			t.Fatalf("tools called: %v", ft.ops)
		}
	}
}
func TestPeerFailuresAreRedacted(t *testing.T) {
	secret := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	ft := &fakeTools{err: map[string]error{"set-peer:peer": errors.New("PSK " + secret), "remove-peer:peer": errors.New("PSK " + secret)}}
	r := New(ft)
	for _, err := range []error{r.UpsertPeer(context.Background(), &amneziaproto.Credential{PublicKey: "peer", DesiredStatus: "active", AssignedIpv4: "10.8.1.2"}), r.RemovePeer(context.Background(), "peer")} {
		if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[redacted]") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}
func TestPruneStopsOnRuntimeFailure(t *testing.T) {
	ft := &fakeTools{err: map[string]error{"show-dump": errors.New("unavailable")}}
	if err := New(ft).PruneRuntimePeers(context.Background(), nil); err == nil {
		t.Fatal("runtime error ignored")
	}
}
