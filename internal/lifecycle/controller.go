package lifecycle

import "fmt"

// Valid state transitions for tools.
var validTransitions = map[string]map[string]bool{
	"pending":                {"enabled": true, "disabled": true, "requires_confirmation": true},
	"enabled":               {"disabled": true, "requires_confirmation": true, "pending": true},
	"disabled":              {"enabled": true, "requires_confirmation": true, "pending": true},
	"requires_confirmation": {"enabled": true, "disabled": true, "pending": true},
}

// ValidateTransition checks whether moving from state "from" to state "to" is
// allowed. Returns nil when the transition is valid.
func ValidateTransition(from, to string) error {
	targets, ok := validTransitions[from]
	if !ok {
		return fmt.Errorf("unknown state %q", from)
	}
	if !targets[to] {
		return fmt.Errorf("invalid transition from %q to %q", from, to)
	}
	return nil
}
