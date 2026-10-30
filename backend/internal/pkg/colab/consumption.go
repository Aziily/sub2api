package colab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Consumption is account-wide, including sessions outside this platform.
// Endpoint also used by googlecolab/google-colab-cli's `colab usage`.
type Consumption struct {
	Balance     *float64 `json:"currentBalance"`
	Rate        *float64 `json:"consumptionRateHourly"`
	Assignments *int     `json:"assignmentsCount"`
}

func (c *Client) Consumption(ctx context.Context) (*Consumption, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://colab.research.google.com/tun/m/ccu-info?authuser=0", nil)
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Colab-Client-Agent", "colab-cli")
	h := &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.HTTP != nil {
		h.Transport = c.HTTP.Transport
	}
	r, e := h.Do(req)
	if e != nil {
		return nil, errors.New("Google CU request failed")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, &APIError{Status: r.StatusCode, Reason: "CU unavailable"}
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, 65537))
	if e != nil || len(b) > 65536 {
		return nil, errors.New("Invalid Google CU response")
	}
	var v Consumption
	if json.Unmarshal([]byte(strings.TrimPrefix(string(b), ")]}'\n")), &v) != nil || v.Balance == nil || math.IsNaN(*v.Balance) || math.IsInf(*v.Balance, 0) || *v.Balance < 0 {
		return nil, errors.New("Google CU balance missing or invalid")
	}
	return &v, nil
}
