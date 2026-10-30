package colab

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type consumeTransport func(*http.Request) (*http.Response, error)

func (f consumeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestConsumption(t *testing.T) {
	for _, tc := range []struct {
		body   string
		status int
		ok     bool
	}{
		{`)]}'` + "\n" + `{"currentBalance":199.9,"consumptionRateHourly":0,"assignmentsCount":0}`, 200, true},
		{`{"currentBalance":0}`, 200, true}, {`{}`, 200, false}, {`{"currentBalance":-1}`, 200, false}, {`denied`, 403, false},
	} {
		c := Client{Token: "private", HTTP: &http.Client{Transport: consumeTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host != "colab.research.google.com" || r.Header.Get("Authorization") != "Bearer private" {
				t.Fatal("wrong credential destination")
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
		})}}
		v, e := c.Consumption(context.Background())
		if (e == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.body, e)
		}
		if tc.ok && v.Balance == nil {
			t.Fatal("missing balance")
		}
	}
}
