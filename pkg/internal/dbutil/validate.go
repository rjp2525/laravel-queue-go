package dbutil

import (
	"fmt"
	"regexp"
)

var validTableName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func ValidateTableName(name string) error {
	if !validTableName.MatchString(name) {
		return fmt.Errorf("invalid table name %q", name)
	}
	return nil
}
