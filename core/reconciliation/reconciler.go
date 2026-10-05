package reconciliation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/horizon/orion/core/domain"
	"github.com/horizon/orion/core/ports"
)

type Mode string

const (
	ModeObserve  Mode = "OBSERVE"
	ModeReport   Mode = "REPORT"
	ModeEnforce  Mode = "ENFORCE"
)

type ReconcileConfig struct {
	Interval time.Duration
	Mode     Mode
}

type Reconciler struct {
	resourceRepo ports.ResourceRepository
	runtime     ResourceReconcileRuntime
	config      ReconcileConfig
	mu          sync.RWMutex
	running     bool
	stopCh      chan struct{}
}

type ResourceReconcileRuntime interface {
	ObserveResource(ctx context.Context, resourceID string) (*domain.Resource, error)
	InvokeResource(ctx context.Context, providerID, operation string, input json.RawMessage) (*ReconcileResult, error)
}

type ReconcileResult struct {
	Success   bool
	Output    json.RawMessage
	ErrorCode string
	ErrorMsg  string
}

func NewReconciler(resourceRepo ports.ResourceRepository, runtime ResourceReconcileRuntime, config ReconcileConfig) *Reconciler {
	if config.Interval == 0 {
		config.Interval = 30 * time.Second
	}
	if config.Mode == "" {
		config.Mode = ModeObserve
	}
	return &Reconciler{
		resourceRepo: resourceRepo,
		runtime:     runtime,
		config:      config,
		stopCh:      make(chan struct{}),
	}
}

func (r *Reconciler) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.mu.Unlock()

	go r.runLoop(ctx)
	log.Printf("Reconciler started in mode %s with interval %s", r.config.Mode, r.config.Interval)
}

func (r *Reconciler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	close(r.stopCh)
	log.Println("Reconciler stopped")
}

func (r *Reconciler) runLoop(ctx context.Context) {
	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.reconcileAll(ctx)
		}
	}
}

func (r *Reconciler) reconcileAll(ctx context.Context) {
	resources, err := r.resourceRepo.ListAll(ctx)
	if err != nil {
		log.Printf("Failed to list resources for reconciliation: %v", err)
		return
	}

	for _, res := range resources {
		if res.State == domain.ResourceStateDeleting || res.State == domain.ResourceStateDeleted {
			continue
		}

		if err := r.reconcileResource(ctx, res); err != nil {
			log.Printf("Failed to reconcile resource %s: %v", res.ID, err)
		}
	}
}

func (r *Reconciler) reconcileResource(ctx context.Context, res *domain.Resource) error {
	observed, err := r.runtime.ObserveResource(ctx, res.ID)
	if err != nil {
		return fmt.Errorf("observe failed: %w", err)
	}

	drifted, driftDetails := r.detectDrift(res, observed)

	if !drifted && res.State == domain.ResourceStateAvailable {
		return nil
	}

	log.Printf("Resource %s: drift=%v, state=%s, mode=%s", res.ID, drifted, res.State, r.config.Mode)

	switch r.config.Mode {
	case ModeObserve:
		return r.handleObserve(ctx, res, observed, driftDetails)

	case ModeReport:
		return r.handleReport(ctx, res, observed, driftDetails)

	case ModeEnforce:
		return r.handleEnforce(ctx, res, observed, driftDetails)
	}

	return nil
}

func (r *Reconciler) detectDrift(desired *domain.Resource, observed *domain.Resource) (bool, *DriftDetails) {
	details := &DriftDetails{}

	if observed.State != desired.State {
		details.StateChanged = true
		details.OldState = string(desired.State)
		details.NewState = string(observed.State)
	}

	if !jsonEqual(desired.DesiredSpec, observed.ActualState) {
		details.SpecDrifted = true
	}

	drifted := details.StateChanged || details.SpecDrifted
	return drifted, details
}

type DriftDetails struct {
	StateChanged bool
	OldState    string
	NewState    string
	SpecDrifted bool
}

func (r *Reconciler) handleObserve(ctx context.Context, res *domain.Resource, observed *domain.Resource, details *DriftDetails) error {
	res.ActualState = observed.ActualState
	res.ObservedGeneration = observed.Generation
	now := time.Now()
	res.ObservedAt = &now

	if details.StateChanged {
		res.State = observed.State
	}

	return r.resourceRepo.Update(ctx, res)
}

func (r *Reconciler) handleReport(ctx context.Context, res *domain.Resource, observed *domain.Resource, details *DriftDetails) error {
	res.ActualState = observed.ActualState
	res.ObservedGeneration = observed.Generation
	now := time.Now()
	res.ObservedAt = &now

	if details.StateChanged {
		res.State = observed.State
	}

	if details.SpecDrifted && res.State != domain.ResourceStateError {
		res.State = domain.ResourceStateUpdating
	}

	return r.resourceRepo.Update(ctx, res)
}

func (r *Reconciler) handleEnforce(ctx context.Context, res *domain.Resource, observed *domain.Resource, details *DriftDetails) error {
	res.ActualState = observed.ActualState
	res.ObservedGeneration = observed.Generation
	now := time.Now()
	res.ObservedAt = &now

	if !details.SpecDrifted && !details.StateChanged {
		return r.resourceRepo.Update(ctx, res)
	}

	res.State = domain.ResourceStateUpdating
	if err := r.resourceRepo.Update(ctx, res); err != nil {
		return err
	}

	result, err := r.runtime.InvokeResource(ctx, res.ProviderID, "update", res.DesiredSpec)
	if err != nil {
		res.State = domain.ResourceStateError
		res.Generation++
		return r.resourceRepo.Update(ctx, res)
	}

	if !result.Success {
		res.State = domain.ResourceStateError
		res.Generation++
		return r.resourceRepo.Update(ctx, res)
	}

	res.State = domain.ResourceStateAvailable
	res.ActualState = result.Output
	res.ObservedGeneration = res.Generation

	return r.resourceRepo.Update(ctx, res)
}

func (r *Reconciler) ReconcileOne(ctx context.Context, resourceID string) error {
	res, err := r.resourceRepo.Get(ctx, resourceID)
	if err != nil {
		return fmt.Errorf("resource not found: %w", err)
	}
	return r.reconcileResource(ctx, res)
}

func jsonEqual(a, b json.RawMessage) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return string(a) == string(b)
}
