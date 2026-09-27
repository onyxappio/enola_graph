package graphsession

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/enola-labs/enola/internal/graphstream"
)

const ownerDigestVersion = "resolved-owner-record-sha256-v2"

// Hash complete encoded contributions, including resolved targets and occurrence
// identifiers. Record order is immaterial; duplicate records remain significant.
func resolvedOwnerDigest(nodes []graphstream.Node, edges []graphstream.Edge) (string, error) {
	// Fixed-width, type-tagged record hashes avoid re-encoding a potentially
	// large array of escaped JSON strings. Sorting preserves multiset semantics.
	records := make([][sha256.Size + 1]byte, 0, len(nodes)+len(edges))
	add := func(kind byte, value any) error {
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(b)
		var record [sha256.Size + 1]byte
		record[0] = kind
		copy(record[1:], digest[:])
		records = append(records, record)
		return nil
	}
	for _, node := range nodes {
		if err := add('n', node); err != nil {
			return "", err
		}
	}
	for _, edge := range edges {
		if err := add('e', edge); err != nil {
			return "", err
		}
	}
	sort.Slice(records, func(i, j int) bool { return bytes.Compare(records[i][:], records[j][:]) < 0 })
	h := sha256.New()
	h.Write([]byte(ownerDigestVersion))
	for i := range records {
		h.Write(records[i][:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
