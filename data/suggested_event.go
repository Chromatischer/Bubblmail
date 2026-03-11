package data

// SuggestedEvent stores a TipiCal-style event proposal extracted from an email.
type SuggestedEvent struct {
	MessageID    int64
	HasEvent     bool
	Summary      string
	Date         string
	Start        string
	End          string
	Location     string
	Calendar     string
	Status       string
	AllDay       bool
	Recurring    bool
	Description  string
	Model        string
	GeneratedAt  int64
	SourceHash   string
	PlainText    string
	JSONText     string
	ParseError   string
	GenerationOK bool
}
