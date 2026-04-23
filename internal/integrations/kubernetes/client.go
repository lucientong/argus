// Package kubernetes defines the interface for querying Kubernetes state.
package kubernetes

import (
	"context"

	"github.com/lucientong/argus/internal/types"
)

// PodInfo holds per-pod state.
type PodInfo struct {
	Name         string
	Namespace    string
	Phase        string // Running, Pending, Failed, Succeeded, Unknown
	RestartCount int32
	Ready        bool
}

// Client is the interface for querying Kubernetes cluster state.
type Client interface {
	// GetDeployment returns the K8sInfo for the given deployment in the namespace.
	GetDeployment(ctx context.Context, namespace, deployment string) (*types.K8sInfo, error)

	// ListPods returns all pods matching the given label selector in a namespace.
	ListPods(ctx context.Context, namespace, selector string) ([]PodInfo, error)

	// GetRecentEvents returns recent Kubernetes events for a namespace.
	GetRecentEvents(ctx context.Context, namespace string, limit int) ([]string, error)

	// GetRecentDeploys returns recent deployment events (rollout history).
	GetRecentDeploys(ctx context.Context, namespace, deployment string) ([]types.DeployEvent, error)
}

// MockClient is an in-memory Client for unit tests.
type MockClient struct {
	Info       *types.K8sInfo
	Pods       []PodInfo
	Events     []string
	Deploys    []types.DeployEvent
	Err        error
}

func (m *MockClient) GetDeployment(_ context.Context, _, _ string) (*types.K8sInfo, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Info, nil
}

func (m *MockClient) ListPods(_ context.Context, _, _ string) ([]PodInfo, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Pods, nil
}

func (m *MockClient) GetRecentEvents(_ context.Context, _ string, _ int) ([]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Events, nil
}

func (m *MockClient) GetRecentDeploys(_ context.Context, _, _ string) ([]types.DeployEvent, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Deploys, nil
}
