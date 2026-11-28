package parse

import (
	"fmt"
	"regexp"
	"strings"
)

var pciID = regexp.MustCompile(`^(?:[1-9][0-9]*|A[1-3])(?:\.[1-9][0-9]*){1,4}$`)
var pciIDCandidate = regexp.MustCompile(`^(?:[0-9]+|A[0-9]+)\.[0-9.]+$`)

type pciNodes struct {
	nodes     []Node
	seen      map[string]int
	root      string
	active    int
	activeRow int
	rows      map[string]bool
}

func (s *pciNodes) add(id, title string, root bool) error {
	if s.seen == nil {
		s.seen = make(map[string]int)
		s.active = -1
		s.activeRow = -1
		s.rows = make(map[string]bool)
	}
	if _, ok := s.seen[id]; ok {
		return fmt.Errorf("duplicate requirement")
	}
	parent := -1
	if root {
		s.root = id
	} else {
		if !pciID.MatchString(id) {
			return fmt.Errorf("invalid requirement")
		}
		dot := strings.IndexByte(id, '.')
		if dot < 0 || id[:dot] != s.root {
			return fmt.Errorf("wrong requirement root")
		}
		parentID := id[:strings.LastIndexByte(id, '.')]
		var ok bool
		parent, ok = s.seen[parentID]
		if !ok {
			return fmt.Errorf("missing requirement parent")
		}
		active := s.active
		for active >= 0 && active != parent {
			active = s.nodes[active].Parent
		}
		if active < 0 {
			return fmt.Errorf("inactive requirement parent")
		}
	}
	s.seen[id] = len(s.nodes)
	s.nodes = append(s.nodes, Node{Citation: id, Kind: "requirement", Parent: parent, Ordinal: len(s.nodes), PublisherTitle: title, Status: "active"})
	s.active = len(s.nodes) - 1
	return nil
}
