package data

import (
	"regexp"
	"strings"
)

// ParsedSearchQuery represents a structured search with optional filter chips and free text.
type ParsedSearchQuery struct {
	From      []string // from: filter values
	To        []string // to: filter values
	Subject   string   // subject: filter
	HasAttach bool     // has:attachment
	FreeText  string   // remaining free-text query
}

var chipRe = regexp.MustCompile(`\[(\w+):([^\]]*)\]`)

// ParseSearchQuery parses a compiled search string like
// "[from:alice@x.com] [has:attachment] some text" into a ParsedSearchQuery.
func ParseSearchQuery(q string) ParsedSearchQuery {
	var pq ParsedSearchQuery
	stripped := q
	for _, m := range chipRe.FindAllStringSubmatch(q, -1) {
		key := strings.ToLower(m[1])
		val := strings.TrimSpace(m[2])
		switch key {
		case "from":
			if val != "" {
				pq.From = append(pq.From, val)
			}
		case "to":
			if val != "" {
				pq.To = append(pq.To, val)
			}
		case "subject":
			pq.Subject = val
		case "has":
			if strings.ToLower(val) == "attachment" {
				pq.HasAttach = true
			}
		}
		stripped = strings.Replace(stripped, m[0], "", 1)
	}
	pq.FreeText = strings.TrimSpace(stripped)
	return pq
}
