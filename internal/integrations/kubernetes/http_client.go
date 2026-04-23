package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/lucientong/argus/internal/types"
)

// HTTPClient queries the Kubernetes API server directly via raw HTTP.
// It supports both in-cluster (service-account token) and kubeconfig-based auth.
type HTTPClient struct {
	baseURL    string
	namespace  string
	token      string
	httpClient *http.Client
}

// NewHTTPClient creates a Kubernetes client.
// If kubeconfigPath is empty it falls back to in-cluster credentials
// ($KUBERNETES_SERVICE_HOST + /var/run/secrets/kubernetes.io/serviceaccount/token).
func NewHTTPClient(kubeconfigPath, namespace string) (*HTTPClient, error) {
	c := &HTTPClient{
		namespace: namespace,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}

	if kubeconfigPath != "" {
		// Minimal kubeconfig parsing: just server + token (no cert verification for simplicity).
		kc, err := loadKubeconfig(kubeconfigPath)
		if err != nil {
			return nil, err
		}
		c.baseURL = kc.server
		c.token = kc.token
	} else {
		// In-cluster: use the environment variables set by Kubernetes.
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host == "" {
			return nil, fmt.Errorf("kubernetes: not running in-cluster and no kubeconfig path provided")
		}
		c.baseURL = "https://" + host + ":" + port
		token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			return nil, fmt.Errorf("kubernetes: read service account token: %w", err)
		}
		c.token = string(token)
	}

	return c, nil
}

// GetDeployment returns K8sInfo for a deployment.
func (c *HTTPClient) GetDeployment(ctx context.Context, namespace, deployment string) (*types.K8sInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/deployments/%s", namespace, deployment)
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var d k8sDeployment
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("kubernetes: decode deployment: %w", err)
	}

	return &types.K8sInfo{
		Namespace:     namespace,
		Deployment:    deployment,
		ReadyReplicas: d.Status.ReadyReplicas,
		TotalReplicas: d.Status.Replicas,
	}, nil
}

// ListPods returns pods matching the label selector.
func (c *HTTPClient) ListPods(ctx context.Context, namespace, selector string) ([]PodInfo, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods", namespace)
	if selector != "" {
		path += "?labelSelector=" + selector
	}
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var list k8sPodList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("kubernetes: decode pod list: %w", err)
	}

	var pods []PodInfo
	for _, p := range list.Items {
		ready := false
		var restarts int32
		for _, cs := range p.Status.ContainerStatuses {
			restarts += cs.RestartCount
			if cs.Ready {
				ready = true
			}
		}
		pods = append(pods, PodInfo{
			Name:         p.Metadata.Name,
			Namespace:    p.Metadata.Namespace,
			Phase:        p.Status.Phase,
			RestartCount: restarts,
			Ready:        ready,
		})
	}
	return pods, nil
}

// GetRecentEvents returns recent warning events for a namespace.
func (c *HTTPClient) GetRecentEvents(ctx context.Context, namespace string, limit int) ([]string, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/events?fieldSelector=type=Warning", namespace)
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var list k8sEventList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("kubernetes: decode event list: %w", err)
	}

	// Sort newest first.
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].LastTimestamp.After(list.Items[j].LastTimestamp)
	})

	var events []string
	for i, e := range list.Items {
		if limit > 0 && i >= limit {
			break
		}
		events = append(events, fmt.Sprintf("[%s] %s/%s: %s",
			e.LastTimestamp.Format(time.RFC3339),
			e.InvolvedObject.Kind,
			e.InvolvedObject.Name,
			e.Message,
		))
	}
	return events, nil
}

// GetRecentDeploys returns rollout history for a deployment via ReplicaSet annotations.
func (c *HTTPClient) GetRecentDeploys(ctx context.Context, namespace, deployment string) ([]types.DeployEvent, error) {
	if namespace == "" {
		namespace = c.namespace
	}
	// List ReplicaSets owned by the deployment.
	path := fmt.Sprintf("/apis/apps/v1/namespaces/%s/replicasets?labelSelector=app=%s", namespace, deployment)
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var list k8sReplicaSetList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("kubernetes: decode replicaset list: %w", err)
	}

	var deploys []types.DeployEvent
	for _, rs := range list.Items {
		revision := rs.Metadata.Annotations["deployment.kubernetes.io/revision"]
		if revision == "" {
			continue
		}
		image := ""
		if len(rs.Spec.Template.Spec.Containers) > 0 {
			image = rs.Spec.Template.Spec.Containers[0].Image
		}
		deploys = append(deploys, types.DeployEvent{
			Service:    deployment,
			Version:    "rev-" + revision + " " + image,
			DeployedAt: rs.Metadata.CreationTimestamp,
		})
	}
	// Sort newest first.
	sort.Slice(deploys, func(i, j int) bool {
		return deploys[i].DeployedAt.After(deploys[j].DeployedAt)
	})
	return deploys, nil
}

// get performs a GET against the API server and returns the body.
func (c *HTTPClient) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: get %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("kubernetes: unexpected status %d for %s", resp.StatusCode, path)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: read body %s: %w", path, err)
	}
	return body, nil
}

// --------------- minimal kubeconfig loader --------------------------------

type kubeconfigData struct {
	server string
	token  string
}

// loadKubeconfig extracts server + token from a KUBECONFIG file.
// This is intentionally minimal — just enough for a bearer-token workflow.
func loadKubeconfig(path string) (*kubeconfigData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("kubernetes: read kubeconfig: %w", err)
	}

	// We avoid a full YAML dependency by using the json package on the
	// already-declared yaml.v3 import — but we're not in config package here.
	// Use a simple struct-based decode instead.
	var kc struct {
		Clusters []struct {
			Cluster struct {
				Server string `json:"server"`
			} `json:"cluster"`
		} `json:"clusters"`
		Users []struct {
			User struct {
				Token string `json:"token"`
			} `json:"user"`
		} `json:"users"`
	}

	// Try JSON first (some tools export kubeconfigs as JSON).
	if jsonErr := json.Unmarshal(data, &kc); jsonErr != nil {
		// Fallback: just scan for server/token lines with naive parsing.
		return parseKubeconfigYAML(data)
	}
	if len(kc.Clusters) == 0 {
		return parseKubeconfigYAML(data)
	}

	result := &kubeconfigData{}
	if len(kc.Clusters) > 0 {
		result.server = kc.Clusters[0].Cluster.Server
	}
	if len(kc.Users) > 0 {
		result.token = kc.Users[0].User.Token
	}
	if result.server == "" {
		return nil, fmt.Errorf("kubernetes: no server found in kubeconfig %s", path)
	}
	return result, nil
}

// parseKubeconfigYAML does a line-by-line scan for "server:" and "token:" fields.
// This avoids adding a YAML import to this package.
func parseKubeconfigYAML(data []byte) (*kubeconfigData, error) {
	result := &kubeconfigData{}
	lines := splitLines(data)
	for _, line := range lines {
		trimmed := trimSpaces(line)
		if hasPrefix(trimmed, "server: ") {
			result.server = trimPrefix(trimmed, "server: ")
		}
		if hasPrefix(trimmed, "token: ") {
			result.token = trimPrefix(trimmed, "token: ")
		}
	}
	if result.server == "" {
		return nil, fmt.Errorf("kubernetes: no server found in kubeconfig")
	}
	return result, nil
}

// tiny string helpers to avoid importing strings package.
func splitLines(b []byte) []string {
	var lines []string
	start := 0
	for i, c := range b {
		if c == '\n' {
			lines = append(lines, string(b[start:i]))
			start = i + 1
		}
	}
	if start < len(b) {
		lines = append(lines, string(b[start:]))
	}
	return lines
}

func trimSpaces(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

// --------------- K8s API response types -----------------------------------

type k8sDeployment struct {
	Status struct {
		Replicas      int32 `json:"replicas"`
		ReadyReplicas int32 `json:"readyReplicas"`
	} `json:"status"`
}

type k8sPodList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Status struct {
			Phase            string `json:"phase"`
			ContainerStatuses []struct {
				Ready        bool  `json:"ready"`
				RestartCount int32 `json:"restartCount"`
			} `json:"containerStatuses"`
		} `json:"status"`
	} `json:"items"`
}

type k8sEventList struct {
	Items []struct {
		LastTimestamp  time.Time `json:"lastTimestamp"`
		Message        string    `json:"message"`
		InvolvedObject struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"involvedObject"`
	} `json:"items"`
}

type k8sReplicaSetList struct {
	Items []struct {
		Metadata struct {
			Name              string            `json:"name"`
			CreationTimestamp time.Time         `json:"creationTimestamp"`
			Annotations       map[string]string `json:"annotations"`
		} `json:"metadata"`
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Image string `json:"image"`
					} `json:"containers"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
	} `json:"items"`
}

