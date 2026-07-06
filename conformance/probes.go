package conformance

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// RedisPing dials hostPort, sends a RESP PING, and waits for `+PONG`.
// Stdlib-only so a plugin author can wire it up without taking a
// dependency on a redis client library.
//
// Plugin authors who prefer go-redis can ignore this helper entirely
// and pass their own NativeProbe — the suite is happy with any func
// that returns a non-nil error on protocol-level unreachability.
func RedisPing(ctx context.Context, hostPort string) error {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		return fmt.Errorf("redis probe: dial %s: %w", hostPort, err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	}
	// RESP inline command form: `PING\r\n`. Servers respond `+PONG\r\n`.
	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		return fmt.Errorf("redis probe: write PING: %w", err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("redis probe: read reply: %w", err)
	}
	if !strings.HasPrefix(reply, "+PONG") {
		return fmt.Errorf("redis probe: want +PONG, got %q", strings.TrimSpace(reply))
	}
	return nil
}

// ElasticsearchGetRoot issues `GET /` against
// http://<hostPort>/ and requires a 200 response. ES returns 200 on
// `/` once the cluster is yellow-or-better; single-node clusters are
// always yellow, so a green response is the canonical "ready for
// queries" signal that no sniff-aware client can dispute.
//
// Why this matters: v0.2.6 shipped because nothing tested that an
// HTTP client running on the host could actually reach the bough-
// allocated port. AssertReachable proves the TCP socket is open;
// this probe proves the protocol round-trip works end-to-end.
func ElasticsearchGetRoot(ctx context.Context, hostPort string) error {
	url := "http://" + hostPort + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("elasticsearch probe: new request: %w", err)
	}
	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return fmt.Errorf("elasticsearch probe: GET %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("elasticsearch probe: GET %s status=%d, want 200", url, resp.StatusCode)
	}
	return nil
}

// pgSSLRequest is the 8-byte PostgreSQL SSLRequest packet: a 4-byte
// big-endian length (8) followed by the 4-byte magic 80877103
// (0x04d2162f). A client may send it as the very first thing on a fresh
// connection, before any startup/auth message, and the server always
// answers with a single byte — 'S' (SSL offered) or 'N' (declined). It
// is therefore the cheapest stdlib-only proof that the peer speaks the
// postgres wire protocol and is past its initdb bootstrap rather than a
// half-open entrypoint.
var pgSSLRequest = []byte{0x00, 0x00, 0x00, 0x08, 0x04, 0xd2, 0x16, 0x2f}

// PostgresProbe dials hostPort and performs a PostgreSQL SSLRequest
// handshake, requiring the server's single-byte 'S'/'N' reply. Stdlib-
// only, so a plugin author can wire it up without a pq/pgx dependency.
// Unlike mysql, postgres sends no unsolicited greeting — the client
// must speak first — which is why an SSLRequest (answerable before
// auth) is the minimal round-trip that proves the protocol layer.
//
// Why the retry loop mirrors the mysql probe: the official postgres
// image runs an initdb bootstrap (temporary server for setup → shutdown
// → exec the final postmaster), and ReadyCheck can go green against the
// temporary server. A probe landing between the shutdown and the final
// exec sees the TCP connect succeed but the read return EOF; the loop
// rides out that ~1-3 s window without weakening the contract.
func PostgresProbe(ctx context.Context, hostPort string) error {
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var lastErr error
	for time.Now().Before(deadline) {
		if err := postgresSSLProbeOnce(ctx, hostPort); err == nil {
			return nil
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("postgres probe: never completed an SSLRequest handshake on %s within 30s, last err: %w",
		hostPort, lastErr)
}

func postgresSSLProbeOnce(ctx context.Context, hostPort string) error {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		return fmt.Errorf("dial %s: %w", hostPort, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(pgSSLRequest); err != nil {
		return fmt.Errorf("write SSLRequest to %s: %w", hostPort, err)
	}
	var reply [1]byte
	if _, err := io.ReadFull(conn, reply[:]); err != nil {
		return fmt.Errorf("read SSLRequest reply from %s: %w", hostPort, err)
	}
	if reply[0] != 'S' && reply[0] != 'N' {
		return fmt.Errorf("SSLRequest reply = %#x, want 'S' or 'N' (PostgreSQL wire protocol)", reply[0])
	}
	return nil
}
