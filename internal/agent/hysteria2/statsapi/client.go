package statsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

type TrafficRecord struct {
	CredentialID string `json:"credential_id"`
	TXBytes      int64  `json:"tx_bytes"`
	RXBytes      int64  `json:"rx_bytes"`
}

type OnlineRecord struct {
	CredentialID string `json:"credential_id"`
	Count        int    `json:"count"`
}

func New(baseURL, secret string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) Traffic(ctx context.Context, clear bool) ([]TrafficRecord, error) {
	u := c.baseURL + "/traffic"
	if clear {
		u += "?clear=1"
	}
	var raw map[string]struct {
		TX int64 `json:"tx"`
		RX int64 `json:"rx"`
	}
	if err := c.get(ctx, u, &raw); err != nil {
		var alt map[string]struct {
			TX int64 `json:"tx_bytes"`
			RX int64 `json:"rx_bytes"`
		}
		if err2 := c.get(ctx, u, &alt); err2 != nil {
			return nil, err
		}
		out := make([]TrafficRecord, 0, len(alt))
		for id, rec := range alt {
			out = append(out, TrafficRecord{CredentialID: id, TXBytes: rec.TX, RXBytes: rec.RX})
		}
		return out, nil
	}
	out := make([]TrafficRecord, 0, len(raw))
	for id, rec := range raw {
		out = append(out, TrafficRecord{CredentialID: id, TXBytes: rec.TX, RXBytes: rec.RX})
	}
	return out, nil
}

func (c *Client) Online(ctx context.Context) ([]OnlineRecord, error) {
	var raw map[string]int
	if err := c.get(ctx, c.baseURL+"/online", &raw); err != nil {
		return nil, err
	}
	out := make([]OnlineRecord, 0, len(raw))
	for id, count := range raw {
		out = append(out, OnlineRecord{CredentialID: id, Count: count})
	}
	return out, nil
}

func (c *Client) Kick(ctx context.Context, credentialID string) error {
	form := url.Values{}
	form.Set("id", credentialID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/kick", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.auth(req)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("hysteria stats api returned %s", res.Status)
	}
	return nil
}

func (c *Client) DumpStreams(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/dump/streams", nil)
	if err != nil {
		return "", err
	}
	c.auth(req)
	res, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("hysteria stats api returned %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 128*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Client) get(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	c.auth(req)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("hysteria stats api returned %s", res.Status)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *Client) auth(req *http.Request) {
	if c.secret != "" {
		req.Header.Set("Authorization", c.secret)
	}
}
