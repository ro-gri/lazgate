package amnezia

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"laz/internal/model"
	"laz/internal/services/connections/remote"
)

type client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) remote.Provider {
	return &client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type clientRecord struct {
	Username string       `json:"username"`
	Peers    []clientPeer `json:"peers"`
}

type clientPeer struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Status     string   `json:"status"`
	AllowedIPs []string `json:"allowedIps"`
	Protocol   string   `json:"protocol"`
}

type listClientsResponse struct {
	Total int            `json:"total"`
	Items []clientRecord `json:"items"`
}

type createClientResponse struct {
	Message string `json:"message"`
	Client  struct {
		ID       string `json:"id"`
		Config   string `json:"config"`
		Protocol string `json:"protocol"`
	} `json:"client"`
}

func (c *client) listClients(ctx context.Context) (listClientsResponse, error) {
	var out listClientsResponse
	err := c.do(ctx, http.MethodGet, "/clients", nil, &out)
	return out, err
}

func (c *client) createClient(ctx context.Context, name string) (createClientResponse, error) {
	body := map[string]any{
		"clientName": name,
		"protocol":   "amneziawg2",
		"expiresAt":  nil,
	}
	var out createClientResponse
	err := c.do(ctx, http.MethodPost, "/clients", body, &out)
	return out, err
}

func (c *client) CreateConnection(ctx context.Context, input remote.CreateInput) (remote.Connection, error) {
	created, err := c.createClient(ctx, input.Name)
	if err != nil {
		return remote.Connection{}, err
	}
	configName := input.ConfigName
	if configName == "" {
		configName = input.Name + " AmneziaWG"
	}
	return remote.Connection{
		Ref: remote.Ref{ID: created.Client.ID, Name: input.Name},
		Configs: []remote.Config{{
			Kind:        model.ConfigAmneziaVPN,
			Slug:        "amnezia",
			Name:        configName,
			Client:      "amnezia",
			ContentType: "text/plain; charset=utf-8",
			Value:       created.Client.Config,
		}},
	}, nil
}

func (c *client) setClientStatus(ctx context.Context, clientID, status string) error {
	body := map[string]any{
		"clientId": clientID,
		"protocol": "amneziawg2",
		"status":   status,
	}
	return c.do(ctx, http.MethodPatch, "/clients", body, nil)
}

func (c *client) SetConnectionStatus(ctx context.Context, ref remote.Ref, status model.Status) error {
	remoteStatus := "active"
	if status == model.StatusHeld {
		remoteStatus = "disabled"
	}
	return c.setClientStatus(ctx, ref.ID, remoteStatus)
}

func (c *client) deleteClient(ctx context.Context, clientID string) error {
	body := map[string]any{
		"clientId": clientID,
		"protocol": "amneziawg2",
	}
	return c.do(ctx, http.MethodDelete, "/clients", body, nil)
}

func (c *client) DeleteConnection(ctx context.Context, ref remote.Ref) error {
	return c.deleteClient(ctx, ref.ID)
}

func (c *client) ListConnections(ctx context.Context) (remote.ConnectionList, error) {
	clients, err := c.listClients(ctx)
	if err != nil {
		return remote.ConnectionList{}, err
	}
	out := remote.ConnectionList{Total: clients.Total}
	for _, item := range clients.Items {
		info := remote.ConnectionInfo{
			Ref:  remote.Ref{Name: item.Username},
			Name: item.Username,
			Raw:  item,
		}
		if len(item.Peers) > 0 {
			info.Ref.ID = item.Peers[0].ID
			info.Status = item.Peers[0].Status
		}
		out.Items = append(out.Items, info)
	}
	if out.Total == 0 {
		out.Total = len(out.Items)
	}
	return out, nil
}

func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("amnezia-api %s %s returned %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
