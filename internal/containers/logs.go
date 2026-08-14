package containers

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Logs opens a following log stream for a container (stdout+stderr, last `tail`
// lines then live). Returns the stream (caller must Close) and whether it is a
// raw TTY stream (true) or Docker's multiplexed stream-frame format (false).
func (s *Service) Logs(ctx context.Context, id string, tail int) (io.ReadCloser, bool, error) {
	tty := s.hasTTY(ctx, id)
	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&follow=1&tail=%d",
		url.PathEscape(id), tail)
	req, err := s.newRequest(ctx, path)
	if err != nil {
		return nil, false, err
	}
	resp, err := s.stream.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("docker unreachable: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, false, fmt.Errorf("docker logs status %d", resp.StatusCode)
	}
	return resp.Body, tty, nil
}

// hasTTY reports whether the container was started with a TTY (changes the log
// stream framing). Best-effort: defaults to false (multiplexed) on error.
func (s *Service) hasTTY(ctx context.Context, id string) bool {
	req, err := s.newRequest(ctx, "/containers/"+url.PathEscape(id)+"/json")
	if err != nil {
		return false
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var info struct {
		Config struct {
			Tty bool `json:"Tty"`
		} `json:"Config"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false
	}
	return info.Config.Tty
}

// StreamLines reads a Docker log stream and invokes emit once per complete text
// line. It blocks until the stream ends (or the underlying reader is closed,
// which is how the caller cancels on client disconnect). Raw when tty, else it
// strips the 8-byte stream-frame headers.
func StreamLines(r io.Reader, tty bool, emit func(string)) error {
	if tty {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			emit(sc.Text())
		}
		return sc.Err()
	}

	// Multiplexed: [stream(1)][000][size uint32 BE][payload...] repeated.
	header := make([]byte, 8)
	var line []byte
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if len(line) > 0 {
				emit(string(line))
			}
			return err
		}
		n := binary.BigEndian.Uint32(header[4:8])
		payload := make([]byte, n)
		if _, err := io.ReadFull(r, payload); err != nil {
			return err
		}
		line = append(line, payload...)
		for {
			i := indexByte(line, '\n')
			if i < 0 {
				break
			}
			emit(string(line[:i]))
			line = line[i+1:]
		}
	}
}

func indexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}
