package imap

import (
	"fmt"

	imaplib "github.com/emersion/go-imap/v2"
)

// FolderStatus returns unread and total counts for a folder.
func (c *Client) FolderStatus(folder string) (int, int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	status, err := c.conn.Status(folder, &imaplib.StatusOptions{
		NumMessages: true,
		NumUnseen:   true,
	}).Wait()
	if err != nil {
		return 0, 0, fmt.Errorf("getting status for %q: %w", folder, err)
	}
	unread := 0
	if status.NumUnseen != nil {
		unread = int(*status.NumUnseen)
	}
	total := 0
	if status.NumMessages != nil {
		total = int(*status.NumMessages)
	}
	return unread, total, nil
}
