package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
)

type Requirements struct {
	NodeID     string   `json:"node_id,omitempty"`
	Snapshots  *bool    `json:"snapshots,omitempty"`
	Encryption *bool    `json:"encryption,omitempty"`
	MinSize    *uint64  `json:"min_size,omitempty"`
	MaxSize    *uint64  `json:"max_size,omitempty"`
	Location   string   `json:"location,omitempty"`
	Affinity   []string `json:"affinity,omitempty"`
}

type Candidate struct {
	Provider   *domain.Provider
	Score      float64
	Reasons    []string
}

type Scheduler struct {
	providerRepo ports.ProviderRepository
	resourceRepo ports.ResourceRepository
}

func NewScheduler(providerRepo ports.ProviderRepository, resourceRepo ports.ResourceRepository) *Scheduler {
	return &Scheduler{
		providerRepo: providerRepo,
		resourceRepo: resourceRepo,
	}
}

func (s *Scheduler) SelectProvider(ctx context.Context, kind, version string, req *Requirements) (*domain.Provider, error) {
	candidates, err := s.FindCandidates(ctx, kind, version, req)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable provider found for kind=%s version=%s", kind, version)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates[0].Provider, nil
}

func (s *Scheduler) FindCandidates(ctx context.Context, kind, version string, req *Requirements) ([]*Candidate, error) {
	providers, err := s.providerRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	var candidates []*Candidate
	for _, p := range providers {
		if p.AdministrativeState != domain.ProviderStateEnabled {
			continue
		}

		if p.HealthState != domain.HealthStateHealthy && p.HealthState != domain.HealthStateUnknown {
			continue
		}

		candidate, err := s.evaluateProvider(p, kind, version, req)
		if err != nil || candidate == nil {
			continue
		}

		candidates = append(candidates, candidate)
	}

	return candidates, nil
}

func (s *Scheduler) evaluateProvider(p *domain.Provider, kind, version string, req *Requirements) (*Candidate, error) {
	var caps map[string]interface{}
	if err := json.Unmarshal(p.EffectiveCapabilities, &caps); err != nil {
		return nil, err
	}

	candidate := &Candidate{
		Provider: p,
		Score:    100.0,
		Reasons:  []string{},
	}

	if req == nil {
		return candidate, nil
	}

	if req.NodeID != "" && p.NodeID != req.NodeID {
		return nil, nil
	}

	if req.Affinity != nil {
		hasAffinity := false
		for _, nodeID := range req.Affinity {
			if p.NodeID == nodeID {
				hasAffinity = true
				break
			}
		}
		if !hasAffinity {
			return nil, nil
		}
		candidate.Score += 30
		candidate.Reasons = append(candidate.Reasons, "node affinity match")
	}

	if req.Snapshots != nil && *req.Snapshots {
		if snapCap, ok := caps["snapshots"]; ok {
			if snapCap == true {
				candidate.Reasons = append(candidate.Reasons, "supports snapshots")
			} else {
				return nil, nil
			}
		} else {
			return nil, nil
		}
	}

	if req.Encryption != nil && *req.Encryption {
		if encCap, ok := caps["encryption"]; ok {
			if encCap == true {
				candidate.Reasons = append(candidate.Reasons, "supports encryption")
			} else {
				return nil, nil
			}
		} else {
			return nil, nil
		}
	}

	if p.HealthState == domain.HealthStateHealthy {
		candidate.Score += 20
		candidate.Reasons = append(candidate.Reasons, "healthy")
	}

	return candidate, nil
}

func (s *Scheduler) SelectMultiple(ctx context.Context, kind, version string, req *Requirements, count int) ([]*domain.Provider, error) {
	candidates, err := s.FindCandidates(ctx, kind, version, req)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no suitable providers found")
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	if count > len(candidates) {
		count = len(candidates)
	}

	result := make([]*domain.Provider, count)
	for i := 0; i < count; i++ {
		result[i] = candidates[i].Provider
	}

	return result, nil
}
