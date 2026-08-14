package containers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Service talks to the Docker Engine over its unix socket using a minimal
// stdlib HTTP client — deliberately NOT the docker/docker SDK, to keep the
// binary small on constrained hardware. Read-only for Phase 1.
type Service struct {
	socketPath string
	http       *http.Client // short requests (list/ping/inspect)
	stream     *http.Client // long-lived (log follow) — no client timeout
	selfHost   string       // our own container's hostname (short id) for self-detection
}

// New builds a Service for the given Docker socket path.
func New(socketPath string) *Service {
	host, _ := os.Hostname()
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}
	return &Service{
		socketPath: socketPath,
		selfHost:   host,
		http: &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{DialContext: dial},
		},
		// No Timeout: log follow is long-lived; cancellation is via context /
		// closing the response body.
		stream: &http.Client{
			Transport: &http.Transport{DialContext: dial},
		},
	}
}

// Container is the read-only view the UI needs.
type Container struct {
	ID     string
	Name   string
	Image  string
	State  string // running, exited, paused, ...
	Status string // "Up 3 minutes"
	Ports  []string
	IsSelf bool // this gateway-manager container (guard against self-actions)
}

// Available reports whether the Docker socket is reachable (for graceful UI).
func (s *Service) Available(ctx context.Context) bool {
	req, err := s.newRequest(ctx, "/_ping")
	if err != nil {
		return false
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// dockerContainer mirrors the subset of GET /containers/json we use.
type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Ports  []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
}

// List returns all containers (running and stopped), sorted by name.
func (s *Service) List(ctx context.Context) ([]Container, error) {
	req, err := s.newRequest(ctx, "/containers/json?all=1")
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker api status %d", resp.StatusCode)
	}

	var raw []dockerContainer
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode containers: %w", err)
	}

	out := make([]Container, 0, len(raw))
	for _, c := range raw {
		out = append(out, Container{
			ID:     shortID(c.ID),
			Name:   cleanName(c.Names),
			Image:  c.Image,
			State:  c.State,
			Status: c.Status,
			Ports:  formatPorts(c),
			IsSelf: s.selfHost != "" && strings.HasPrefix(c.ID, s.selfHost),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// newRequest builds a request against the socket (host is ignored for unix).
func (s *Service) newRequest(ctx context.Context, path string) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// cleanName takes the first name and strips the leading "/".
func cleanName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func formatPorts(c dockerContainer) []string {
	seen := map[string]bool{}
	var ports []string
	for _, p := range c.Ports {
		var s string
		if p.PublicPort != 0 {
			s = fmt.Sprintf("%d→%d/%s", p.PublicPort, p.PrivatePort, p.Type)
		} else {
			s = fmt.Sprintf("%d/%s", p.PrivatePort, p.Type)
		}
		if !seen[s] {
			seen[s] = true
			ports = append(ports, s)
		}
	}
	return ports
}
