package data

// Tag represents a local-only label applied to a message.
type Tag struct {
	ID    int64
	Name  string
	Color string // hex color for badge rendering
}

// WellKnownTags are pre-defined tag names with semantic meaning.
const (
	TagImportant = "important"
	TagWork      = "work"
	TagPersonal  = "personal"
	TagTodo      = "todo"
	TagWaiting   = "waiting"
)
