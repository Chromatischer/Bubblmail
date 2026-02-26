package data

// Folder represents an IMAP folder/mailbox.
type Folder struct {
	Name        string
	DisplayName string
	Delimiter   string
	Attributes  []string
	Depth       int
	Unread      int
	Total       int
	AccountName string
}

// IsSelectable returns true if the folder can be selected (not just a container).
func (f *Folder) IsSelectable() bool {
	for _, attr := range f.Attributes {
		if attr == `\Noselect` {
			return false
		}
	}
	return true
}

// IsInbox returns true if this is the INBOX folder.
func (f *Folder) IsInbox() bool {
	return f.Name == "INBOX"
}
