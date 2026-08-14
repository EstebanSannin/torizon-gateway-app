package containers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// IsSelf reports whether the given id refers to this gateway-manager container
// (used to refuse actions that would stop the app itself). Both values are
// short container ids / hostnames.
func (s *Service) IsSelf(id string) bool {
	if s.selfHost == "" || id == "" {
		return false
	}
	return strings.HasPrefix(id, s.selfHost) || strings.HasPrefix(s.selfHost, id)
}

// Start starts a stopped container.
func (s *Service) Start(ctx context.Context, id string) error {
	return s.action(ctx, id, "start")
}

// Stop stops a running container (Docker's default grace period).
func (s *Service) Stop(ctx context.Context, id string) error {
	return s.action(ctx, id, "stop")
}

// Restart restarts a container.
func (s *Service) Restart(ctx context.Context, id string) error {
	return s.action(ctx, id, "restart")
}

// action POSTs to /containers/{id}/{verb}. Docker returns 204 on success and
// 304 when the container is already in the target state — both are non-errors.
func (s *Service) action(ctx context.Context, id, verb string) error {
	path := fmt.Sprintf("/containers/%s/%s", url.PathEscape(id), verb)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker unreachable: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusNotModified:
		return nil
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("docker %s failed (%d): %s", verb, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}
