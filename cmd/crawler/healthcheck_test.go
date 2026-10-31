package main

import "testing"

func TestLoopbackOnly(t *testing.T) {
	ok := []string{"http://127.0.0.1:8080/readyz", "http://localhost:3000/healthz", "http://[::1]:8080/x"}
	bad := []string{"http://10.0.0.1/readyz", "http://example.com/", "file:///etc/passwd", "gopher://127.0.0.1", "http://127.0.0.1.evil.example/"}
	for _, u := range ok {
		if err := loopbackOnly(u); err != nil {
			t.Errorf("%s rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if loopbackOnly(u) == nil {
			t.Errorf("%s accepted", u)
		}
	}
}
