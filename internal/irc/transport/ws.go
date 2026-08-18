package transport

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// IRC over WebSocket (the IRCv3 websocket transport): each IRC line is one
// text frame, without CRLF. wsConn adapts that framing to the stream-style
// net.Conn that ircevent expects.

type wsConn struct {
	*websocket.Conn
	buf  []byte // unread remainder of the current frame
	full []byte // reusable backing array
}

func (w *wsConn) Read(p []byte) (int, error) {
	if len(w.buf) == 0 {
		_, data, err := w.Conn.ReadMessage()
		if err != nil {
			return 0, err
		}
		// reuse our buffer: the empty-length gate proves it is drained
		w.full = append(w.full[:0], data...)
		w.full = append(w.full, '\r', '\n')
		w.buf = w.full
	}
	n := copy(p, w.buf)
	w.buf = w.buf[n:]
	return n, nil
}

func (w *wsConn) Write(p []byte) (int, error) {
	for _, line := range bytes.Split(p, []byte("\r\n")) {
		if len(line) == 0 {
			continue
		}
		if err := w.Conn.WriteMessage(websocket.TextMessage, line); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *wsConn) SetDeadline(t time.Time) error {
	if err := w.Conn.SetReadDeadline(t); err != nil {
		return err
	}
	return w.Conn.SetWriteDeadline(t)
}

// IsWebSocketURL reports whether the server field is a ws:// or wss:// URL.
func IsWebSocketURL(server string) bool {
	return strings.HasPrefix(server, "ws://") || strings.HasPrefix(server, "wss://")
}

// DisplayHost extracts the hostname for display and buffer naming.
func DisplayHost(rawURL string) string {
	if u, err := url.Parse(rawURL); err == nil && u.Host != "" {
		return u.Host
	}
	return rawURL
}

// Dial opens an IRC-over-WebSocket connection.
func Dial(ctx context.Context, rawURL string) (net.Conn, error) {
	d := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
		Subprotocols:     []string{"text.ircv3.net"},
	}
	c, _, err := d.DialContext(ctx, rawURL, http.Header{})
	if err != nil {
		return nil, err
	}
	// a hostile server must not be able to balloon memory with one frame
	c.SetReadLimit(512 * 1024)
	return &wsConn{Conn: c}, nil
}
