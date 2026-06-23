package evolve

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/ikeikeikeike/bough/pkg/schema"
)

// Cluster groups TraceBundles that share enough token overlap to
// represent the same recurring lesson. Each cluster is what the LLM
// judge evaluates as a unit — small clusters are demoted to DOUBT,
// large + diverse clusters cross the PASS threshold.
type Cluster struct {
	ID                  string
	Members             []schema.TraceBundle
	MemberHashes        []string
	NearestPriorLabel   string
	NearestPriorDesc    string
}

// JaccardThreshold is the Gate 3 similarity boundary. Two bundles
// share a cluster when their token-set Jaccard similarity ≥ 0.4
// (= the ECC Python v3 value). Lower threshold = larger clusters
// = more false-positive promotions; higher = singleton-heavy.
const JaccardThreshold = 0.4

// Gate3Cluster groups the input bundles by Jaccard similarity on
// their normalised token sets. The implementation is the simple
// O(N²) sweep ECC ships — adequate at the v0.7.1 corpus sizes
// (~ 300-500 candidates per evolve run) and easy to diff against
// the Python reference output.
//
// priors carries the existing instinct labels + descriptions from
// the memory backend so each cluster can hang a NearestPriorLabel
// link off it. Pass nil to skip the prior-link step.
func Gate3Cluster(bundles []schema.TraceBundle, priors []PriorLabel) []Cluster {
	if len(bundles) == 0 {
		return nil
	}
	tokSets := make([]map[string]struct{}, len(bundles))
	for i, b := range bundles {
		tokSets[i] = tokensetOf(b.Content)
	}

	assigned := make([]int, len(bundles))
	for i := range assigned {
		assigned[i] = -1
	}
	clusters := []Cluster{}
	for i := range bundles {
		if assigned[i] != -1 {
			continue
		}
		cid := len(clusters)
		assigned[i] = cid
		c := Cluster{ID: clusterID(cid), Members: []schema.TraceBundle{bundles[i]}}
		c.MemberHashes = append(c.MemberHashes, hashContent(bundles[i].Content))
		for j := i + 1; j < len(bundles); j++ {
			if assigned[j] != -1 {
				continue
			}
			if jaccard(tokSets[i], tokSets[j]) >= JaccardThreshold {
				assigned[j] = cid
				c.Members = append(c.Members, bundles[j])
				c.MemberHashes = append(c.MemberHashes, hashContent(bundles[j].Content))
			}
		}
		if priors != nil {
			if lp := nearestPrior(c.Members, priors); lp.Label != "" {
				c.NearestPriorLabel = lp.Label
				c.NearestPriorDesc = lp.Description
			}
		}
		clusters = append(clusters, c)
	}
	// Sort clusters by size descending so the audit log surfaces
	// the most-evidenced clusters first.
	sort.SliceStable(clusters, func(i, j int) bool {
		return len(clusters[i].Members) > len(clusters[j].Members)
	})
	return clusters
}

// PriorLabel is a (label, description) pair the evolve pipeline
// pulls from the memory backend before clustering, so each new
// cluster can stack on the nearest existing instinct rather than
// minting a fresh label.
type PriorLabel struct {
	Label       string
	Description string
}

func clusterID(idx int) string {
	h := sha256.Sum256([]byte("cluster-" + hex.EncodeToString([]byte{byte(idx)})))
	return hex.EncodeToString(h[:8])
}

func hashContent(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func tokensetOf(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range tokenize(s) {
		out[t] = struct{}{}
	}
	return out
}

func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if _, ok := b[k]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// nearestPrior picks the prior whose description shares the most
// tokens with any of the cluster members. Returns zero-value
// PriorLabel when no prior crosses the JaccardThreshold.
func nearestPrior(members []schema.TraceBundle, priors []PriorLabel) PriorLabel {
	bestScore := JaccardThreshold
	best := PriorLabel{}
	for _, p := range priors {
		ptok := tokensetOf(p.Description)
		for _, m := range members {
			mtok := tokensetOf(m.Content)
			if s := jaccard(ptok, mtok); s > bestScore {
				bestScore = s
				best = p
			}
		}
	}
	return best
}
