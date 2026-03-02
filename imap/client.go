// Package imap wraps go-imap/v2 with tea.Cmd builders for async operations.
package imap

import (
	"crypto/tls"
	"fmt"
	"sync"

	imaplib "github.com/emersion/go-imap/v2/imapclient"
	"github.com/bubblmail/bubblmail/config"
)

// Client wraps an IMAP connection for a single account.
// mu serializes all IMAP operations so concurrent tea.Cmd goroutines don't
// interleave SELECT + command sequences and slow each other down.
type Client struct {
	cfg  *config.AccountConfig
	conn *imaplib.Client
	mu   sync.Mutex
}

// Connect establishes a TLS IMAP connection.
func Connect(cfg *config.AccountConfig) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", cfg.IMAPHost, cfg.IMAPPort)

	tlsCfg := &tls.Config{ServerName: cfg.IMAPHost}
	conn, err := imaplib.DialTLS(addr, &imaplib.Options{
		TLSConfig: tlsCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", addr, err)
	}

	password, err := cfg.ResolvePassword()
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("resolving password: %w", err)
	}

	if err := conn.Login(cfg.Username, password).Wait(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("IMAP login failed: %w", err)
	}

	return &Client{cfg: cfg, conn: conn}, nil
}

// Close logs out and closes the connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	c.conn.Logout()
	return c.conn.Close()
}

// AccountName returns the configured account name.
func (c *Client) AccountName() string {
	return c.cfg.Name
}
