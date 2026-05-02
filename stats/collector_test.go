package stats

import (
	"encoding/json"
	"testing"
)

func TestCollectorTracksDomainsAndRoutes(t *testing.T) {
	c := New()

	c.IncDomain("example.com", true)
	c.IncDomain("example.com", true)
	c.IncDomain("example.com", false)
	c.AddDomainBytes("example.com", 100, 200)
	c.IncDomain("github.com", true)

	c.IncRoute("*.example.com", true)
	c.AddRouteBytes("*.example.com", 50, 75)
	c.IncRoute("direct", false)

	snap := c.Snapshot()

	if d := snap.Domains["example.com"]; d.Success != 2 || d.Failures != 1 || d.BytesIn != 100 || d.BytesOut != 200 {
		t.Errorf("example.com domain stats = %+v, want success=2 failures=1 bytes_in=100 bytes_out=200", d)
	}
	if d := snap.Domains["github.com"]; d.Success != 1 {
		t.Errorf("github.com.Success = %d, want 1", d.Success)
	}
	if r := snap.Routes["*.example.com"]; r.Success != 1 || r.BytesIn != 50 || r.BytesOut != 75 {
		t.Errorf("*.example.com route stats = %+v, want success=1 bytes_in=50 bytes_out=75", r)
	}
	if r := snap.Routes["direct"]; r.Failures != 1 {
		t.Errorf("direct route failures = %d, want 1", r.Failures)
	}
}

func TestCollectorIgnoresEmptyDomainAndRoute(t *testing.T) {
	// Some handler paths (e.g. peek_failed, no SNI) reach the
	// instrumentation site without a usable hostname/route. Pass
	// "" and verify nothing is recorded so we don't pollute the
	// reports with anonymous entries.
	c := New()
	c.IncDomain("", true)
	c.AddDomainBytes("", 100, 200)
	c.IncRoute("", true)
	c.AddRouteBytes("", 100, 200)

	snap := c.Snapshot()
	if len(snap.Domains) != 0 {
		t.Errorf("Domains = %v, want empty", snap.Domains)
	}
	if len(snap.Routes) != 0 {
		t.Errorf("Routes = %v, want empty", snap.Routes)
	}
}

func TestCollectorGlobalCounters(t *testing.T) {
	c := New()

	c.IncProtocol("HTTP")
	c.IncProtocol("HTTP")
	c.IncProtocol("CONNECT")
	c.IncProtocol("TLS")
	c.IncError("parse_failed")
	c.IncError("parse_failed")
	c.IncError("dial_failed")

	snap := c.Snapshot()

	if snap.Connections != 4 {
		t.Errorf("Connections = %d, want 4", snap.Connections)
	}
	if snap.Protocols["HTTP"] != 2 {
		t.Errorf("Protocols[HTTP] = %d, want 2", snap.Protocols["HTTP"])
	}
	if snap.Protocols["CONNECT"] != 1 {
		t.Errorf("Protocols[CONNECT] = %d, want 1", snap.Protocols["CONNECT"])
	}
	if snap.Protocols["TLS"] != 1 {
		t.Errorf("Protocols[TLS] = %d, want 1", snap.Protocols["TLS"])
	}
	if snap.Errors["parse_failed"] != 2 {
		t.Errorf("Errors[parse_failed] = %d, want 2", snap.Errors["parse_failed"])
	}
	if snap.Errors["dial_failed"] != 1 {
		t.Errorf("Errors[dial_failed] = %d, want 1", snap.Errors["dial_failed"])
	}
}

func TestCollectorUpstreamCounters(t *testing.T) {
	c := New()

	c.IncUpstream("hq", true)
	c.IncUpstream("hq", true)
	c.IncUpstream("hq", false)
	c.IncUpstream("direct", true)
	c.AddUpstreamBytes("hq", 1000, 2000)
	c.AddUpstreamBytes("hq", 500, 300)
	c.AddUpstreamBytes("direct", 100, 200)

	snap := c.Snapshot()

	hq, ok := snap.Upstreams["hq"]
	if !ok {
		t.Fatal("missing upstream 'hq'")
	}
	if hq.Success != 2 {
		t.Errorf("hq.Success = %d, want 2", hq.Success)
	}
	if hq.Failures != 1 {
		t.Errorf("hq.Failures = %d, want 1", hq.Failures)
	}
	if hq.BytesIn != 1500 {
		t.Errorf("hq.BytesIn = %d, want 1500", hq.BytesIn)
	}
	if hq.BytesOut != 2300 {
		t.Errorf("hq.BytesOut = %d, want 2300", hq.BytesOut)
	}

	direct, ok := snap.Upstreams["direct"]
	if !ok {
		t.Fatal("missing upstream 'direct'")
	}
	if direct.Success != 1 {
		t.Errorf("direct.Success = %d, want 1", direct.Success)
	}
	if direct.BytesIn != 100 {
		t.Errorf("direct.BytesIn = %d, want 100", direct.BytesIn)
	}
}

func TestCollectorActiveConnections(t *testing.T) {
	c := New()

	c.IncActive()
	c.IncActive()
	c.IncActive()
	c.DecActive()

	snap := c.Snapshot()
	if snap.Active != 2 {
		t.Errorf("Active = %d, want 2", snap.Active)
	}
}

func TestSnapshotJSON(t *testing.T) {
	c := New()
	c.IncProtocol("HTTP")
	c.IncUpstream("direct", true)

	snap := c.Snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.Connections != 1 {
		t.Errorf("decoded.Connections = %d, want 1", decoded.Connections)
	}
	if decoded.Upstreams["direct"].Success != 1 {
		t.Errorf("decoded upstream direct success = %d, want 1", decoded.Upstreams["direct"].Success)
	}
}

func TestCollectorPrePopulatedProtocols(t *testing.T) {
	c := New()
	snap := c.Snapshot()

	// Known protocols should exist in the snapshot even before any traffic
	for _, proto := range []string{"TLS", "HTTP", "CONNECT", "WebSocket"} {
		if _, ok := snap.Protocols[proto]; !ok {
			t.Errorf("expected pre-populated protocol %q in snapshot", proto)
		}
	}

	// Known error types should exist
	for _, errType := range []string{"peek_failed", "parse_failed", "sni_failed", "dial_failed", "bad_request"} {
		if _, ok := snap.Errors[errType]; !ok {
			t.Errorf("expected pre-populated error type %q in snapshot", errType)
		}
	}
}

func TestCollectorPrePopulatedProtocolsAccumulate(t *testing.T) {
	c := New()
	c.IncProtocol("HTTP")
	c.IncProtocol("HTTP")

	snap := c.Snapshot()
	if snap.Protocols["HTTP"] != 2 {
		t.Errorf("Protocols[HTTP] = %d, want 2", snap.Protocols["HTTP"])
	}
	// Other pre-populated protocols should still be zero
	if snap.Protocols["TLS"] != 0 {
		t.Errorf("Protocols[TLS] = %d, want 0", snap.Protocols["TLS"])
	}
}
