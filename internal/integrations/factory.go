// Package integrations provides a factory for constructing integration clients
// based on the configured mode ("mock" or "real").
package integrations

import (
	"fmt"

	"github.com/lucientong/argus/internal/config"
	"github.com/lucientong/argus/internal/integrations/grafana"
	"github.com/lucientong/argus/internal/integrations/kubernetes"
	"github.com/lucientong/argus/internal/integrations/pagerduty"
	"github.com/lucientong/argus/internal/integrations/prometheus"
	"github.com/lucientong/argus/internal/integrations/slack"
)

// Clients bundles all integration clients together.
type Clients struct {
	Prometheus prometheus.Client
	Kubernetes kubernetes.Client
	Slack      slack.Client
	Grafana    grafana.Client
	PagerDuty  pagerduty.Client
}

// Build constructs integration clients from the given config.
// When cfg.Integrations.Mode == "real", real HTTP clients are used.
// Any other value (including "mock") returns in-memory mock clients.
func Build(cfg *config.Config) (*Clients, error) {
	if cfg.Integrations.Mode == "real" {
		return buildReal(cfg)
	}
	return buildMock(), nil
}

// buildMock returns all mock clients wired for testing / local development.
func buildMock() *Clients {
	return &Clients{
		Prometheus: &prometheus.MockClient{},
		Kubernetes: &kubernetes.MockClient{},
		Slack:      &slack.MockClient{AutoApprove: true},
		Grafana:    &grafana.MockClient{},
		PagerDuty:  &pagerduty.MockClient{},
	}
}

// buildReal creates real HTTP clients for each integration.
func buildReal(cfg *config.Config) (*Clients, error) {
	k8sClient, err := kubernetes.NewHTTPClient(
		cfg.Kubernetes.KubeconfigPath,
		cfg.Kubernetes.Namespace,
	)
	if err != nil {
		return nil, fmt.Errorf("integrations: init kubernetes client: %w", err)
	}

	return &Clients{
		Prometheus: prometheus.NewHTTPClient(cfg.Prometheus.URL),
		Kubernetes: k8sClient,
		Slack:      slack.NewHTTPClient(cfg.Slack.Token),
		Grafana:    grafana.NewHTTPClient(cfg.Grafana.URL, cfg.Grafana.APIKey),
		PagerDuty:  pagerduty.NewHTTPClient(cfg.PagerDuty.IntegrationKey),
	}, nil
}
