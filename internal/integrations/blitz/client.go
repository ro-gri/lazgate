package blitz

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"laz/internal/model"
	"laz/internal/services/connections/remote"
)

type client struct {
	node model.Node
	http *http.Client
}

func New(node model.Node) remote.Provider {
	return &client{
		node: node,
		http: &http.Client{Timeout: 20 * time.Second},
	}
}

type account struct {
	Username            string `json:"username"`
	Password            string `json:"password,omitempty"`
	MaxDownloadBytes    int64  `json:"max_download_bytes,omitempty"`
	ExpirationDays      int    `json:"expiration_days,omitempty"`
	AccountCreationDate string `json:"account_creation_date,omitempty"`
	Blocked             bool   `json:"blocked"`
	UnlimitedUser       bool   `json:"unlimited_user"`
	Note                string `json:"note,omitempty"`
	Status              string `json:"status,omitempty"`
}

type uriResponse struct {
	Username  string    `json:"username"`
	IPv4      string    `json:"ipv4,omitempty"`
	IPv6      string    `json:"ipv6,omitempty"`
	Nodes     []nodeURI `json:"nodes,omitempty"`
	NormalSub string    `json:"normal_sub,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type nodeURI struct {
	Name string `json:"name"`
	URI  string `json:"uri"`
}

type createUserInput struct {
	Username       string `json:"username"`
	Password       string `json:"password,omitempty"`
	TrafficLimitGB int    `json:"traffic_limit"`
	ExpirationDays int    `json:"expiration_days"`
	Unlimited      bool   `json:"unlimited"`
	Note           string `json:"note,omitempty"`
}

func (c *client) listUsers(ctx context.Context) ([]account, error) {
	var out []account
	err := c.do(ctx, http.MethodGet, "/api/v1/users/", nil, &out)
	return out, err
}

func (c *client) createUser(ctx context.Context, input createUserInput) error {
	body := map[string]any{
		"username":        input.Username,
		"traffic_limit":   input.TrafficLimitGB,
		"expiration_days": input.ExpirationDays,
		"password":        input.Password,
		"unlimited":       input.Unlimited,
		"note":            input.Note,
	}
	return c.do(ctx, http.MethodPost, "/api/v1/users/", body, nil)
}

func (c *client) CreateConnection(ctx context.Context, input remote.CreateInput) (remote.Connection, error) {
	if err := c.createUser(ctx, createUserInput{
		Username:       input.Name,
		Password:       input.Password,
		TrafficLimitGB: input.TrafficLimitGB,
		ExpirationDays: input.ExpirationDays,
		Unlimited:      input.Unlimited,
		Note:           input.Note,
	}); err != nil {
		return remote.Connection{}, err
	}
	uri, err := c.userURI(ctx, input.Name)
	if err != nil {
		_ = c.deleteUser(ctx, input.Name)
		return remote.Connection{}, err
	}
	return remote.Connection{
		Ref:     remote.Ref{ID: input.Name, Name: input.Name},
		Configs: c.configsForURI(defaultConfigName(input), uri, input.IncludeIPv6),
	}, nil
}

func (c *client) setUserBlocked(ctx context.Context, username string, blocked bool) error {
	body := map[string]any{"blocked": blocked}
	err := c.do(ctx, http.MethodPatch, "/api/v1/users/"+url.PathEscape(username), body, nil)
	if err == nil || c.node.SSHHost == "" {
		return err
	}
	return c.setUserBlockedWithScript(ctx, username, blocked)
}

func (c *client) SetConnectionStatus(ctx context.Context, ref remote.Ref, status model.Status) error {
	return c.setUserBlocked(ctx, ref.Name, status == model.StatusHeld)
}

func (c *client) deleteUser(ctx context.Context, username string) error {
	err := c.do(ctx, http.MethodDelete, "/api/v1/users/"+url.PathEscape(username), nil, nil)
	if err == nil || c.node.SSHHost == "" {
		return err
	}
	return c.removeUserWithScript(ctx, username)
}

func (c *client) DeleteConnection(ctx context.Context, ref remote.Ref) error {
	return c.deleteUser(ctx, ref.Name)
}

func (c *client) userURI(ctx context.Context, username string) (uriResponse, error) {
	var out uriResponse
	err := c.do(ctx, http.MethodGet, "/api/v1/users/"+url.PathEscape(username)+"/uri", nil, &out)
	return out, err
}

func (c *client) ListConnections(ctx context.Context) (remote.ConnectionList, error) {
	accounts, err := c.listUsers(ctx)
	if err != nil {
		return remote.ConnectionList{}, err
	}
	out := remote.ConnectionList{Total: len(accounts)}
	for _, item := range accounts {
		status := item.Status
		if status == "" && item.Blocked {
			status = "blocked"
		}
		out.Items = append(out.Items, remote.ConnectionInfo{
			Ref:    remote.Ref{ID: item.Username, Name: item.Username},
			Name:   item.Username,
			Status: status,
			Raw:    item,
		})
	}
	return out, nil
}

func (c *client) do(ctx context.Context, method, path string, body any, out any) error {
	if c.node.SSHHost != "" {
		return c.doSSH(ctx, method, path, body, out)
	}
	return c.doHTTP(ctx, method, path, body, out)
}

func (c *client) doHTTP(ctx context.Context, method, path string, body any, out any) error {
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

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.node.BaseURL, "/")+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.node.APIKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("blitz API %s %s returned %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *client) doSSH(ctx context.Context, method, path string, body any, out any) error {
	remote, err := c.remoteCurl(method, path, body)
	if err != nil {
		return err
	}
	stdout, err := c.runRemoteShell(ctx, remote)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(&stdout).Decode(out); err != nil {
		return fmt.Errorf("decode blitz SSH response: %w", err)
	}
	return nil
}

func (c *client) runRemoteShell(ctx context.Context, remote string) (bytes.Buffer, error) {
	args := c.sshArgs(remote)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout, fmt.Errorf("blitz SSH call failed: %s", msg)
	}
	return stdout, nil
}

func (c *client) removeUserWithScript(ctx context.Context, username string) error {
	remote := strings.Join([]string{
		"/etc/hysteria/hysteria2_venv/bin/python3",
		"/etc/hysteria/core/scripts/hysteria2/remove_user.py",
		shellQuote(username),
	}, " ")
	stdout, err := c.runRemoteShell(ctx, remote)
	if err == nil {
		return nil
	}
	if strings.Contains(stdout.String(), "No matching accounts found") {
		return nil
	}
	return err
}

func (c *client) setUserBlockedWithScript(ctx context.Context, username string, blocked bool) error {
	value := "false"
	if blocked {
		value = "true"
	}
	remote := strings.Join([]string{
		"/etc/hysteria/hysteria2_venv/bin/python3",
		"/etc/hysteria/core/scripts/hysteria2/edit_user.py",
		shellQuote(username),
		"--blocked",
		shellQuote(value),
	}, " ")
	_, err := c.runRemoteShell(ctx, remote)
	return err
}

func (c *client) remoteCurl(method, path string, body any) (string, error) {
	endpoint := strings.TrimRight(c.node.BaseURL, "/") + path
	parts := []string{
		"curl", "-fsS", "-X", shellQuote(method),
		"-H", shellQuote("Authorization: " + c.node.APIKey),
	}
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return "", err
		}
		parts = append(parts, "-H", shellQuote("Content-Type: application/json"), "--data", shellQuote(string(raw)))
	}
	parts = append(parts, shellQuote(endpoint))
	return strings.Join(parts, " "), nil
}

func (c *client) sshArgs(remote string) []string {
	port := c.node.SSHPort
	if port == 0 {
		port = 22
	}
	account := c.node.SSHUser
	if account == "" {
		account = "root"
	}
	args := []string{
		"-p", strconv.Itoa(port),
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/app/.ssh/known_hosts",
		"-o", "ConnectTimeout=10",
	}
	if c.node.SSHKeyPath != "" {
		args = append(args, "-i", c.node.SSHKeyPath)
	}
	args = append(args, "--", account+"@"+c.node.SSHHost, "sh -lc "+shellQuote(remote))
	return args
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (c *client) configsForURI(defaultName string, resp uriResponse, includeIPv6 bool) []remote.Config {
	lines := uriLines(resp, includeIPv6)
	configs := make([]remote.Config, 0, len(lines))
	for i, line := range lines {
		name := defaultName
		slug := "hy2"
		if len(resp.Nodes) > i && resp.Nodes[i].Name != "" {
			name = resp.Nodes[i].Name
			slug = slugify(resp.Nodes[i].Name)
		}
		configs = append(configs, remote.Config{
			Kind:        model.ConfigHy2URI,
			Slug:        slug,
			Name:        name,
			Client:      "happ",
			ContentType: "text/plain; charset=utf-8",
			Value:       line,
		})
	}
	if len(configs) == 0 && resp.NormalSub != "" {
		configs = append(configs, remote.Config{
			Kind:        model.ConfigHy2URI,
			Slug:        "normal-sub",
			Name:        defaultName,
			Client:      "happ",
			ContentType: "text/plain; charset=utf-8",
			Value:       resp.NormalSub,
		})
	}
	return configs
}

func defaultConfigName(input remote.CreateInput) string {
	if input.ConfigName != "" {
		return input.ConfigName
	}
	return input.Name + " Hysteria2"
}

func uriLines(resp uriResponse, includeIPv6 bool) []string {
	lines := []string{}
	if resp.IPv4 != "" {
		lines = append(lines, resp.IPv4)
	}
	if includeIPv6 && resp.IPv6 != "" {
		lines = append(lines, resp.IPv6)
	}
	for _, node := range resp.Nodes {
		if node.URI != "" && (includeIPv6 || !uriUsesIPv6(node.URI)) {
			lines = append(lines, node.URI)
		}
	}
	return lines
}

func uriUsesIPv6(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.Contains(raw, "@[")
	}
	return strings.Contains(parsed.Hostname(), ":")
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
