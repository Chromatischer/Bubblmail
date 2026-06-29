package imap

import (
	"github.com/bubblmail/bubblmail/data"
	tea "github.com/charmbracelet/bubbletea"
	imaplib "github.com/emersion/go-imap/v2"
)

// SearchResultMsg carries the result of an IMAP SEARCH.
type SearchResultMsg struct {
	Account  string
	Folder   string
	Messages []*data.Message
	Err      error
}

// SearchIMAP returns a tea.Cmd that performs an IMAP SEARCH and fetches envelopes.
func (c *Client) SearchIMAP(folder, query string) tea.Cmd {
	return func() tea.Msg {
		c.mu.Lock()
		defer c.mu.Unlock()
		msgs, err := c.searchIMAP(folder, query)
		return SearchResultMsg{Account: c.cfg.Name, Folder: folder, Messages: msgs, Err: err}
	}
}

func (c *Client) searchIMAP(folder, query string) ([]*data.Message, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}

	pq := data.ParseSearchQuery(query)
	criteria := &imaplib.SearchCriteria{}

	// Free-text: search body or full message text
	if pq.FreeText != "" {
		criteria.Or = [][2]imaplib.SearchCriteria{
			{
				{Text: []string{pq.FreeText}},
				{Body: []string{pq.FreeText}},
			},
		}
	}

	// From / To / Subject header filters
	for _, addr := range pq.From {
		criteria.Header = append(criteria.Header, imaplib.SearchCriteriaHeaderField{Key: "From", Value: addr})
	}
	for _, addr := range pq.To {
		criteria.Header = append(criteria.Header, imaplib.SearchCriteriaHeaderField{Key: "To", Value: addr})
	}
	if pq.Subject != "" {
		criteria.Header = append(criteria.Header, imaplib.SearchCriteriaHeaderField{Key: "Subject", Value: pq.Subject})
	}

	searchOpts := &imaplib.SearchOptions{
		ReturnAll: true,
	}
	result, err := c.conn.Search(criteria, searchOpts).Wait()
	if err != nil {
		return nil, err
	}

	seqNums := result.AllSeqNums()
	if len(seqNums) == 0 {
		return nil, nil
	}
	if len(seqNums) > 50 {
		seqNums = seqNums[len(seqNums)-50:]
	}

	seqSet := imaplib.SeqSet{}
	for _, n := range seqNums {
		seqSet.AddNum(n)
	}

	fetchOptions := &imaplib.FetchOptions{
		Envelope:   true,
		Flags:      true,
		RFC822Size: true,
		UID:        true,
	}

	buffers, err := c.conn.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		return nil, err
	}

	result2 := make([]*data.Message, 0, len(buffers))
	for _, buf := range buffers {
		result2 = append(result2, convertMessageBuffer(buf, c.cfg.Name, folder))
	}
	return result2, nil
}
