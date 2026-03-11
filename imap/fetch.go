package imap

import (
	"fmt"
	"io"
	"mime"
	"strings"
	"unicode/utf8"

	"github.com/bubblmail/bubblmail/data"
	tea "github.com/charmbracelet/bubbletea"
	imaplib "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	gomail "github.com/emersion/go-message/mail"
)

// --- Message types returned by tea.Cmd ---

// FolderListMsg carries the result of FetchFolders.
type FolderListMsg struct {
	Account string
	Folders []*data.Folder
	Err     error
}

// MessageListMsg carries the result of FetchMessages.
type MessageListMsg struct {
	Account  string
	Folder   string
	Messages []*data.Message
	Err      error
}

// MoreMessageListMsg carries older messages appended during infinite scroll.
type MoreMessageListMsg struct {
	Account  string
	Folder   string
	Messages []*data.Message
	Err      error
}

// MessageBodyMsg carries the result of FetchBody.
type MessageBodyMsg struct {
	Account     string
	Folder      string
	UID         uint32
	MsgID       int64
	Text        string
	HTML        string
	Attachments []data.Attachment
	Err         error
}

// SetFlagResultMsg carries the result of SetFlag.
type SetFlagResultMsg struct {
	Account string
	Folder  string
	UID     uint32
	Flag    data.Flag
	Set     bool
	Err     error
}

// MoveToTrashResultMsg carries the result of MoveToTrash.
type MoveToTrashResultMsg struct {
	Account string
	Folder  string
	UID     uint32
	Err     error
}

// CreateFolderResultMsg carries the result of CreateFolder.
type CreateFolderResultMsg struct {
	Account string
	Name    string
	Err     error
}

// MoveMessageResultMsg carries the result of MoveMessage.
type MoveMessageResultMsg struct {
	Account string
	Folder  string
	UID     uint32
	Dest    string
	Err     error
}

// --- tea.Cmd builders ---

// FetchFolders returns a tea.Cmd that lists all IMAP folders.
func (c *Client) FetchFolders() tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		folders, err := c.fetchFolders()
		return FolderListMsg{Account: c.cfg.Name, Folders: folders, Err: err}
	}
}

// FetchMessages returns a tea.Cmd that fetches the newest message envelopes from a folder.
func (c *Client) FetchMessages(folder string, limit int) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		msgs, err := c.fetchMessages(folder, 0, limit)
		return MessageListMsg{Account: c.cfg.Name, Folder: folder, Messages: msgs, Err: err}
	}
}

// FetchMoreMessages returns a tea.Cmd that fetches older messages beyond those already loaded.
// skip is the count of newest messages already fetched.
func (c *Client) FetchMoreMessages(folder string, skip, limit int) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		msgs, err := c.fetchMessages(folder, skip, limit)
		return MoreMessageListMsg{Account: c.cfg.Name, Folder: folder, Messages: msgs, Err: err}
	}
}

// FetchBody returns a tea.Cmd that fetches the full body of a message.
func (c *Client) FetchBody(folder string, uid uint32, msgID int64) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		text, html, attachments, err := c.fetchBody(folder, uid)
		return MessageBodyMsg{
			Account: c.cfg.Name, Folder: folder,
			UID: uid, MsgID: msgID,
			Text: text, HTML: html, Attachments: attachments, Err: err,
		}
	}
}

// SetFlag returns a tea.Cmd that sets or clears an IMAP flag.
func (c *Client) SetFlag(folder string, uid uint32, flag data.Flag, set bool) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		err := c.setFlag(folder, uid, flag, set)
		return SetFlagResultMsg{Account: c.cfg.Name, Folder: folder, UID: uid, Flag: flag, Set: set, Err: err}
	}
}

// MoveMessage returns a tea.Cmd that moves a message to any destination folder.
func (c *Client) MoveMessage(sourceFolder string, uid uint32, destFolder string) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		err := c.moveToTrash(sourceFolder, uid, destFolder)
		return MoveMessageResultMsg{Account: c.cfg.Name, Folder: sourceFolder, UID: uid, Dest: destFolder, Err: err}
	}
}

// MoveToTrash returns a tea.Cmd that moves a message to the trash folder.
// trashFolder is the full IMAP name of the destination (e.g. "Trash").
func (c *Client) MoveToTrash(sourceFolder string, uid uint32, trashFolder string) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		err := c.moveToTrash(sourceFolder, uid, trashFolder)
		return MoveToTrashResultMsg{Account: c.cfg.Name, Folder: sourceFolder, UID: uid, Err: err}
	}
}

// CreateFolder returns a tea.Cmd that creates a new IMAP mailbox with the given name.
func (c *Client) CreateFolder(name string) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		err := c.conn.Create(name, nil).Wait()
		return CreateFolderResultMsg{Account: c.cfg.Name, Name: name, Err: err}
	}
}

// --- internal implementation ---

func (c *Client) fetchFolders() ([]*data.Folder, error) {
	mailboxes, err := c.conn.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("listing mailboxes: %w", err)
	}

	folders := make([]*data.Folder, 0, len(mailboxes))
	for _, mb := range mailboxes {
		delim := string(mb.Delim)
		depth := 0
		if delim != "" {
			depth = strings.Count(mb.Mailbox, delim)
		}
		parts := strings.Split(mb.Mailbox, delim)
		displayName := parts[len(parts)-1]

		attrs := make([]string, 0, len(mb.Attrs))
		for _, a := range mb.Attrs {
			attrs = append(attrs, string(a))
		}

		folders = append(folders, &data.Folder{
			Name:        mb.Mailbox,
			DisplayName: displayName,
			Delimiter:   delim,
			Attributes:  attrs,
			Depth:       depth,
			AccountName: c.cfg.Name,
		})
	}
	return folders, nil
}

func (c *Client) fetchMessages(folder string, skip, limit int) ([]*data.Message, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	status, err := c.conn.Status(folder, &imaplib.StatusOptions{
		NumMessages: true,
	}).Wait()
	if err != nil {
		return nil, fmt.Errorf("getting status for %q: %w", folder, err)
	}

	total := uint32(0)
	if status.NumMessages != nil {
		total = *status.NumMessages
	}
	if total == 0 || uint32(skip) >= total {
		return nil, nil
	}

	end := total - uint32(skip)
	start := uint32(1)
	if end > uint32(limit) {
		start = end - uint32(limit) + 1
	}

	var seqSet imaplib.SeqSet
	seqSet.AddRange(start, end)
	fetchOptions := &imaplib.FetchOptions{
		Envelope:      true,
		Flags:         true,
		BodyStructure: &imaplib.FetchItemBodyStructure{},
		RFC822Size:    true,
		UID:           true,
	}

	buffers, err := c.conn.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetching messages: %w", err)
	}

	result := make([]*data.Message, 0, len(buffers))
	for _, buf := range buffers {
		m := convertMessageBuffer(buf, c.cfg.Name, folder)
		result = append(result, m)
	}
	return result, nil
}

func (c *Client) fetchBody(folder string, uid uint32) (string, string, []data.Attachment, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return "", "", nil, fmt.Errorf("selecting folder: %w", err)
	}

	uidSet := imaplib.UIDSetNum(imaplib.UID(uid))
	fetchOptions := &imaplib.FetchOptions{
		BodySection: []*imaplib.FetchItemBodySection{
			{Peek: true}, // full message body, don't mark as read again
		},
	}

	buffers, err := c.conn.Fetch(uidSet, fetchOptions).Collect()
	if err != nil {
		return "", "", nil, fmt.Errorf("fetching body: %w", err)
	}
	if len(buffers) == 0 {
		return "", "", nil, fmt.Errorf("message not found")
	}

	buf := buffers[0]
	for _, bs := range buf.BodySection {
		if len(bs.Bytes) == 0 {
			continue
		}
		text, html, attachments, err := parseBody(strings.NewReader(string(bs.Bytes)))
		if err == nil {
			return text, html, attachments, nil
		}
	}
	return "", "", nil, nil
}

func (c *Client) setFlag(folder string, uid uint32, flag data.Flag, set bool) error {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("selecting folder: %w", err)
	}

	uidSet := imaplib.UIDSetNum(imaplib.UID(uid))
	op := imaplib.StoreFlagsAdd
	if !set {
		op = imaplib.StoreFlagsDel
	}
	store := &imaplib.StoreFlags{
		Op:     op,
		Silent: true,
		Flags:  []imaplib.Flag{imaplib.Flag(flag)},
	}
	cmd := c.conn.Store(uidSet, store, nil)
	return cmd.Close()
}

func (c *Client) moveToTrash(sourceFolder string, uid uint32, trashFolder string) error {
	if _, err := c.conn.Select(sourceFolder, nil).Wait(); err != nil {
		return fmt.Errorf("selecting folder: %w", err)
	}
	uidSet := imaplib.UIDSetNum(imaplib.UID(uid))
	// Move handles MOVE extension fallback (COPY + STORE \Deleted + EXPUNGE) automatically.
	_, err := c.conn.Move(uidSet, trashFolder).Wait()
	return err
}

// convertMessageBuffer converts a FetchMessageBuffer to a data.Message.
func convertMessageBuffer(buf *imapclient.FetchMessageBuffer, account, folder string) *data.Message {
	m := &data.Message{
		AccountName: account,
		FolderName:  folder,
		UID:         uint32(buf.UID),
	}

	if buf.RFC822Size > 0 {
		m.Size = uint32(buf.RFC822Size)
	}

	for _, f := range buf.Flags {
		m.Flags = append(m.Flags, data.Flag(f))
	}

	if buf.Envelope != nil {
		env := buf.Envelope
		m.Subject = env.Subject
		m.Date = env.Date
		m.MessageID = env.MessageID
		if len(env.InReplyTo) > 0 {
			m.InReplyTo = env.InReplyTo[0]
		}

		for _, addr := range env.From {
			m.From = append(m.From, convertAddress(addr))
		}
		for _, addr := range env.To {
			m.To = append(m.To, convertAddress(addr))
		}
		for _, addr := range env.Cc {
			m.CC = append(m.CC, convertAddress(addr))
		}
	}

	return m
}

func convertAddress(addr imaplib.Address) data.Address {
	name := addr.Name
	email := ""
	if addr.Mailbox != "" && addr.Host != "" {
		email = addr.Mailbox + "@" + addr.Host
	}
	return data.Address{Name: name, Address: email}
}

// parseBody extracts plain text, HTML, and attachments from a raw RFC 2822 message.
func parseBody(r io.Reader) (string, string, []data.Attachment, error) {
	mr, err := gomail.CreateReader(r)
	if err != nil {
		return "", "", nil, err
	}

	var plainText, htmlText string
	var attachments []data.Attachment
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		ct := part.Header.Get("Content-Type")
		cd := part.Header.Get("Content-Disposition")
		body, err := io.ReadAll(part.Body)
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(ct, "text/plain"):
			if utf8.Valid(body) && plainText == "" {
				plainText = string(body)
			}
		case strings.HasPrefix(ct, "text/html"):
			if utf8.Valid(body) && htmlText == "" {
				htmlText = string(body)
			}
		default:
			name := extractAttachmentFilename(ct, cd)
			// Fallback for Content-Disposition: attachment with no filename.
			if name == "" && cd != "" {
				if dispType, _, err := mime.ParseMediaType(cd); err == nil && dispType == "attachment" {
					name = fallbackFilename(ct)
				}
			}
			// Known non-body content types that may lack explicit disposition.
			if name == "" {
				name = knownAttachmentFilename(ct)
			}
			if name != "" {
				attachments = append(attachments, data.Attachment{
					Filename:    name,
					ContentType: ct,
					Data:        body,
				})
			}
		}
	}

	// Generate a plain-text snippet from HTML if no plain part.
	if plainText == "" && htmlText != "" {
		plainText = stripHTMLTags(htmlText)
	}

	return plainText, htmlText, attachments, nil
}

// extractAttachmentFilename returns the filename from Content-Disposition or Content-Type, or "".
func extractAttachmentFilename(ct, cd string) string {
	if cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if name := params["filename"]; name != "" {
				return name
			}
		}
	}
	if ct != "" {
		if _, params, err := mime.ParseMediaType(ct); err == nil {
			if name := params["name"]; name != "" {
				return name
			}
		}
	}
	return ""
}

// knownAttachmentFilename returns a default filename for content types that are
// always attachments even when no explicit filename is provided (e.g. text/calendar).
func knownAttachmentFilename(ct string) string {
	mediaType := ct
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		mediaType = strings.TrimSpace(ct[:i])
	}
	mediaType = strings.ToLower(mediaType)
	switch mediaType {
	case "text/calendar":
		return "invite.ics"
	}
	return ""
}

// fallbackFilename returns a generic filename for a content type using the MIME
// extension registry, used when Content-Disposition: attachment has no filename.
func fallbackFilename(ct string) string {
	mediaType := ct
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		mediaType = strings.TrimSpace(ct[:i])
	}
	exts, err := mime.ExtensionsByType(mediaType)
	if err == nil && len(exts) > 0 {
		return "attachment" + exts[0]
	}
	return "attachment"
}

// stripHTMLTags removes HTML tags for plain-text fallback.
func stripHTMLTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	result := b.String()
	lines := strings.Split(result, "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
