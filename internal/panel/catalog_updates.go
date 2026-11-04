package panel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// Catalog decisions are part of State: previews, backups and rollback cover them
// in the same transaction as the selected domain changes.
type CatalogUpdate struct {
	ID      string   `json:"id"`
	Digest  string   `json:"digest"`
	Group   Group    `json:"group"`
	Missing []string `json:"missing"`
	New     bool     `json:"new"`
}

func catalogUpdates(st State) []CatalogUpdate {
	out := []CatalogUpdate{}
	for _, specs := range [][]Group{presets, additionalPresets} {
		for _, spec := range specs {
			domains := append([]string{}, spec.Domains...)
			sort.Strings(domains)
			data, _ := json.Marshal(domains)
			sum := sha256.Sum256(data)
			proposal := CatalogUpdate{ID: spec.ID, Digest: hex.EncodeToString(sum[:]), Group: spec, New: true, Missing: []string{}}
			present := map[string]bool{}
			index := -1
			for i, g := range st.Groups {
				if g.ID == spec.ID {
					index = i
					break
				}
			}
			if index < 0 {
				for i, g := range st.Groups {
					for _, d := range g.Domains {
						if d == spec.Domains[0] {
							index = i
							break
						}
					}
					if index >= 0 {
						break
					}
				}
			}
			if index >= 0 {
				proposal.New = false
				proposal.Group = st.Groups[index]
				for _, d := range st.Groups[index].Domains {
					present[d] = true
				}
			} else {
				proposal.Group.Enabled = false
				proposal.Group.Category = inferCategory(spec.ID)
				proposal.Group.Domains = []string{}
			}
			for _, d := range domains {
				if !present[d] {
					proposal.Missing = append(proposal.Missing, d)
				}
			}
			if len(proposal.Missing) > 0 {
				out = append(out, proposal)
			}
		}
	}
	return out
}
