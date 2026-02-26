package data

// Account holds runtime state for a configured email account.
type Account struct {
	Name        string
	Username    string
	IMAPHost    string
	IMAPPort    int
	SMTPHost    string
	SMTPPort    int
	Folders     []*Folder
	ActiveFolder string
}
