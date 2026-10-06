package sup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

var errRemovalCancelled = errors.New("removal cancelled")

func (r projectRuntime) confirmRemoval(p *projectRecord) (bool, error) {
	fmt.Fprintf(r.stderr, "Remove %s and discard its files and sessions? [y/N] ", p.Name)
	if r.in == nil {
		return false, nil
	}
	answer, err := bufio.NewReader(r.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("read removal confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
