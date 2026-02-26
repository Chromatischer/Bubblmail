package imap

import (
	tea "github.com/charmbracelet/bubbletea"
	imaplib "github.com/emersion/go-imap/v2"
	"github.com/bubblmail/bubblmail/data"
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
		msgs, err := c.searchIMAP(folder, query)
		return SearchResultMsg{Account: c.cfg.Name, Folder: folder, Messages: msgs, Err: err}
	}
}

func (c *Client) searchIMAP(folder, query string) ([]*data.Message, error) {
	if _, err := c.conn.Select(folder, nil).Wait(); err != nil {
		return nil, err
	}

	// Search for messages containing the query in body or subject header
	criteria := &imaplib.SearchCriteria{
		Or: [][2]imaplib.SearchCriteria{
			{
				{Text: []string{query}},
				{Body: []string{query}},
			},
		},
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
