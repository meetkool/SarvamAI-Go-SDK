package sarvam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/coder/websocket"
)

type wsConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *Client) dialWS(ctx context.Context, path string, q url.Values) (*wsConn, error) {
	u := strings.Replace(c.baseURL, "http", "ws", 1) + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	h := http.Header{}
	h.Set("api-subscription-key", c.apiKey)
	h.Set("User-Agent", c.userAgent)

	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{HTTPClient: c.http, HTTPHeader: h})
	if err != nil {
		if resp != nil && resp.Body != nil && resp.StatusCode >= 300 {
			defer resp.Body.Close()
			return nil, parseAPIError(resp)
		}
		return nil, fmt.Errorf("sarvam: websocket dial: %w", err)
	}
	conn.SetReadLimit(16 << 20)
	return &wsConn{conn: conn}, nil
}

func (w *wsConn) writeJSON(ctx context.Context, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("sarvam: encoding message: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	err = w.conn.Write(ctx, websocket.MessageText, b)
	if err != nil && isNormalClose(err) {
		return ErrStreamClosed
	}
	return err
}

func (w *wsConn) read(ctx context.Context) ([]byte, error) {
	_, b, err := w.conn.Read(ctx)
	if err != nil {
		if isNormalClose(err) {
			err = io.EOF
		}
		return nil, err
	}
	return b, nil
}
func (w *wsConn) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.Close(websocket.StatusNormalClosure, "")
}

func isNormalClose(err error) bool {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd:
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed)
}
