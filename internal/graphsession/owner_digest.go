package graphsession

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/enola-labs/enola/internal/graphstream"
)

const ownerDigestVersion = "resolved-owner-json-v1"

// Hash complete encoded contributions, including resolved targets and occurrence
// identifiers. Record order is immaterial; duplicate records remain significant.
func resolvedOwnerDigest(nodes []graphstream.Node, edges []graphstream.Edge) (string, error) {
	records := make([]string, 0, len(nodes)+len(edges))
	for _, node := range nodes {
		b, err := json.Marshal(node)
		if err != nil {
			return "", err
		}
		records = append(records, "n:"+string(b))
	}
	for _, edge := range edges {
		b, err := json.Marshal(edge)
		if err != nil {
			return "", err
		}
		records = append(records, "e:"+string(b))
	}
	sort.Strings(records)
	b, err := json.Marshal(records)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s *session) narrowPublishedOwners(grouped []ownerOutput, idx *idIndex, digests map[string]string) error {
	groups := make(map[string]ownerOutput, len(grouped))
	for _, g := range grouped {
		groups[g.Owner.String()] = g
	}
	narrowed := &fileInvalidationPlan{member: map[string]bool{}}
	for _, owner := range s.replaceScope {
		g := groups[owner.String()]
		nodes, edges := encodeOwner(g, idx, false)
		digest, err := resolvedOwnerDigest(nodes, edges)
		if err != nil {
			return err
		}
		digests[owner.String()] = digest
		// Missing/versionless fingerprints are unknown, never proof of neutrality.
		if s.state.OwnerDigestVersion == ownerDigestVersion && s.state.OwnerDigests[owner.String()] == digest {
			continue
		}
		narrowed.owners = append(narrowed.owners, owner)
		narrowed.member[owner.ID] = true
	}
	graphstream.SortOwners(narrowed.owners)
	narrowed.digest = graphstream.DigestOwners(narrowed.owners)
	s.replaceScope = narrowed.manifest()
	s.plan = narrowed
	return nil
}
