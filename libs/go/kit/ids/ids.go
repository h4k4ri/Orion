package ids

import (
	"fmt"

	"uuid"
)

func New(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewV7().String())
}
