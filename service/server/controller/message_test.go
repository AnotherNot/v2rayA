package controller

import (
	"net/http/httptest"
	"testing"
)

func TestWSTokenReadsSubprotocolAndAuthorization(t *testing.T) {
	cases := []struct {
		name            string
		target          string
		subprotocols    string
		authorization   string
		wantToken       string
		wantSubprotocol string
	}{
		{
			name:            "token subprotocol",
			subprotocols:    "v2raya.v1.header.payload.signature",
			wantToken:       "header.payload.signature",
			wantSubprotocol: "v2raya.v1.header.payload.signature",
		},
		{
			name:            "other protocols before the token",
			subprotocols:    "chat, v2raya.v1.header.payload.signature",
			wantToken:       "header.payload.signature",
			wantSubprotocol: "v2raya.v1.header.payload.signature",
		},
		{
			name:          "bearer authorization fallback",
			authorization: "Bearer header.payload.signature",
			wantToken:     "header.payload.signature",
		},
		{
			name:          "bare authorization fallback",
			authorization: "header.payload.signature",
			wantToken:     "header.payload.signature",
		},
		{
			name:   "query token is ignored",
			target: "/api/message?token=header.payload.signature&Authorization=header.payload.signature",
		},
		{
			name: "nothing offered",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			if target == "" {
				target = "/api/message"
			}
			req := httptest.NewRequest("GET", target, nil)
			if tc.subprotocols != "" {
				req.Header.Set("Sec-WebSocket-Protocol", tc.subprotocols)
			}
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			token, subprotocol := wsToken(req)
			if token != tc.wantToken || subprotocol != tc.wantSubprotocol {
				t.Fatalf("wsToken = (%q, %q), want (%q, %q)", token, subprotocol, tc.wantToken, tc.wantSubprotocol)
			}
		})
	}
}
