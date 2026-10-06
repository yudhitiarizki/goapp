package reqctx

import "net/http"

// Transport is an http.RoundTripper that forwards the inbound request id on
// every outbound request, so a call to an external service shares the same
// X-Request-ID and traces line up across systems. Wrap any base transport:
//
//	client := &http.Client{Transport: reqctx.Transport{Base: http.DefaultTransport}}
//
// Adapters only need to build their request with http.NewRequestWithContext so
// the id is on the context — no per-call header wiring.
type Transport struct {
	Base http.RoundTripper
}

func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	if req.Header.Get(HeaderRequestID) == "" {
		if id := FromContext(req.Context()).RequestID; id != "" {
			// Clone before mutating: RoundTrippers must not modify the caller's request.
			req = req.Clone(req.Context())
			req.Header.Set(HeaderRequestID, id)
		}
	}
	return base.RoundTrip(req)
}
