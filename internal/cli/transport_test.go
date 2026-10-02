package cli

import "net/http/httptest"

const pinnedTestFingerprint = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func pinnedTestEntry(name string, server *httptest.Server) TrustEntry {
	return TrustEntry{Name: name, Origin: server.URL, SPKIPin: SPKIFingerprint(server.Certificate())}
}
