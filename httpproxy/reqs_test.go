package httpproxy

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestParseRequestLine(t *testing.T) {
	var (
		req    *request
		err    error
		assert = assert.New(t)
	)
	urls := []string{"GET http://www.google.com?k=v&b=1 HTTP/1.1",
		"POST http://www.google.com:8888/?k=%E4%BD%A0%E5%A5%BD HTTP/1.1",
		"CONNECT www.google.com:443 HTTP/1.0"}
	expectedReqs := []request{
		{
			method:   "GET",
			scheme:   "http",
			host:     "www.google.com",
			port:     80,
			protocol: "HTTP/1.1",
			query:    "k=v&b=1",
		},
		{
			method:   "POST",
			scheme:   "http",
			host:     "www.google.com",
			port:     8888,
			protocol: "HTTP/1.1",
			query:    "k=%E4%BD%A0%E5%A5%BD",
		},
		{
			method:   "CONNECT",
			scheme:   "",
			host:     "www.google.com",
			port:     443,
			protocol: "HTTP/1.0",
			query:    "",
		},
	}
	for idx, u := range urls {
		if req, err = parseRequestLine(u); err != nil {
			t.Errorf("parse url: [%s], error: %s", u, err)
		} else {
			expect := expectedReqs[idx]
			assert.Equal(expect.method, req.method)
			assert.Equal(expect.scheme, req.scheme)
			assert.Equal(expect.host, req.host)
			assert.Equal(expect.port, req.port)
			assert.Equal(expect.protocol, req.protocol)
			assert.Equal(expect.query, req.query)
		}
	}
}
