//go:build integration

package publicsample

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"danny.vn/trachuan/apps/server/internal/publicsummary"
)

func TestHandler_ChunkedBodyRejectedBeforeDrain(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		method  string
		status  int
	}{{false, http.MethodGet, 400}, {true, http.MethodGet, 400}, {true, http.MethodPost, 405}} {
		t.Run(fmt.Sprint(tc.enabled, tc.method), func(t *testing.T) {
			s := httptest.NewServer(NewHandler(tc.enabled, publicsummary.NewAdmission()))
			defer s.Close()
			u, err := url.Parse(s.URL)
			if err != nil {
				t.Fatal(err)
			}
			conn, err := net.DialTimeout("tcp", u.Host, 100*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if _, err := fmt.Fprintf(conn, "%s /api/v1/public/sample HTTP/1.1\r\nHost: example.test\r\nTransfer-Encoding: chunked\r\n\r\n", tc.method); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status %d", response.StatusCode)
			}
		})
	}
}
