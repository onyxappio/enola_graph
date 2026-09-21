package graphsession

import (
	"fmt"
	"path"
	"strings"

	"github.com/enola-labs/enola/internal/graphstream"
)

// fileInvalidationPlan is immutable after construction. Dependencies point from
// a source file to the files its analysis depends on. A fallback domain must be
// proven complete by its caller; nil means no narrower domain is established.
// The current name-based resolver requires the repository-wide fallback.
type fileInvalidationPlan struct {
	owners []graphstream.OwnerRef
	member map[string]bool
	digest string
}

func planFileInvalidation(changed, previous, current []string, dependencies map[string][]string, fallback bool, domain []string) (*fileInvalidationPlan, error) {
	p := &fileInvalidationPlan{member: make(map[string]bool)}
	queue := []string{}
	add := func(name string) error {
		if invalidOwnerPath(name) {
			return fmt.Errorf("invalidation plan: invalid repo-relative file owner %q", name)
		}
		if !p.member[name] {
			p.member[name] = true
			queue = append(queue, name)
		}
		return nil
	}
	for _, name := range changed {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	if fallback && len(changed) > 0 {
		if domain == nil {
			domain = append(append([]string{}, previous...), current...)
		}
		for _, name := range domain {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	reverse := map[string][]string{}
	for file, deps := range dependencies {
		for _, dep := range deps {
			reverse[dep] = append(reverse[dep], file)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, dependent := range reverse[queue[i]] {
			if err := add(dependent); err != nil {
				return nil, err
			}
		}
	}
	for file := range p.member {
		p.owners = append(p.owners, graphstream.OwnerRef{Kind: graphstream.OwnerFile, ID: file})
	}
	graphstream.SortOwners(p.owners)
	p.digest = graphstream.DigestOwners(p.owners)
	return p, nil
}

func (p *fileInvalidationPlan) manifest() []graphstream.OwnerRef {
	return append([]graphstream.OwnerRef(nil), p.owners...)
}

func (p *fileInvalidationPlan) check(owner graphstream.OwnerRef) error {
	if owner.Kind != graphstream.OwnerFile || !p.member[owner.ID] {
		return fmt.Errorf("invalidation plan: owner %s outside frozen scope", owner.String())
	}
	return nil
}

func invalidOwnerPath(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	if strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "../") {
		return true
	}
	if len(name) >= 2 && name[1] == ':' && isDriveLetter(name[0]) {
		return true
	}
	return path.Clean(name) != name
}

func isDriveLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
