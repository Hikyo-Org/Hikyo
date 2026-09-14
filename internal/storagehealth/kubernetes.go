package storagehealth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
)

const (
	kubeletTimeout  = 3 * time.Second
	maxSummaryBytes = 8 << 20
	maxVolumeAge    = 2 * time.Minute
	// Allow small clock skew between the application and storage node.
	maxFutureSkew       = 5 * time.Second
	serviceAccountCA    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	serviceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// KubernetesOptions explicitly binds a kubelet origin to one database PVC.
// The caller must update this mapping when the database moves to another node.
type KubernetesOptions = config.PostgresStorage

// Kubernetes reads fresh filesystem statistics directly from a kubelet. It does
// not contact the Kubernetes nodes/proxy API or substitute local disk capacity.
type Kubernetes struct {
	options KubernetesOptions
	client  *http.Client
	now     func() time.Time
}

// NewKubernetes uses the mounted cluster CA and rotating service-account token.
// Local credential/configuration errors fail startup; network failures are
// returned by Read so diagnostics report unknown without reusing old samples.
func NewKubernetes(options KubernetesOptions) (*Kubernetes, error) {
	return newKubernetes(options, serviceAccountCA, serviceAccountToken)
}

func newKubernetes(options KubernetesOptions, caFile, tokenFile string) (*Kubernetes, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	caData, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read kubelet CA: %w", err)
	}
	if len(caData) == 0 {
		return nil, errors.New("kubelet CA file is empty")
	}
	// Use client-go's standard token-file refresh transport, with no ambient
	// proxy discovery. Nonempty CAData prevents implicit system-root fallback.
	transport, err := rest.TransportFor(&rest.Config{
		Host:            options.KubeletURL,
		TLSClientConfig: rest.TLSClientConfig{CAData: caData},
		BearerTokenFile: tokenFile,
		Proxy:           func(*http.Request) (*url.URL, error) { return nil, nil },
	})
	if err != nil {
		return nil, fmt.Errorf("initialize kubelet credentials: %w", err)
	}
	return &Kubernetes{options: options, now: time.Now, client: &http.Client{
		Transport:     transport,
		Timeout:       kubeletTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

// CloseIdleConnections releases transport connections when a generation retires.
func (k *Kubernetes) CloseIdleConnections() { utilnet.CloseIdleConnectionsFor(k.client.Transport) }

func (k *Kubernetes) Read() (Capacity, error) {
	response, err := k.client.Get(k.options.KubeletURL + "/stats/summary")
	if err != nil {
		return Capacity{}, fmt.Errorf("read kubelet summary: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Capacity{}, fmt.Errorf("kubelet summary returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSummaryBytes+1))
	if err != nil {
		return Capacity{}, fmt.Errorf("read kubelet summary body: %w", err)
	}
	if len(body) > maxSummaryBytes {
		return Capacity{}, errors.New("kubelet summary exceeds size limit")
	}
	return capacityFromSummary(body, k.options, k.now())
}

type kubeletSummary struct {
	Node struct {
		NodeName string `json:"nodeName"`
	} `json:"node"`
	Pods []struct {
		PodRef struct {
			Namespace string `json:"namespace"`
		} `json:"podRef"`
		Volumes []struct {
			Time           time.Time `json:"time"`
			CapacityBytes  *uint64   `json:"capacityBytes"`
			AvailableBytes *uint64   `json:"availableBytes"`
			PVCRef         *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"pvcRef"`
		} `json:"volume"`
	} `json:"pods"`
}

func capacityFromSummary(body []byte, options KubernetesOptions, now time.Time) (Capacity, error) {
	var summary kubeletSummary
	if err := json.Unmarshal(body, &summary); err != nil {
		return Capacity{}, fmt.Errorf("invalid kubelet summary: %w", err)
	}
	if summary.Node.NodeName != options.Node {
		return Capacity{}, errors.New("kubelet summary node does not match configured database node")
	}
	var result Capacity
	found := false
	for _, pod := range summary.Pods {
		if pod.PodRef.Namespace != options.Namespace {
			continue
		}
		for _, volume := range pod.Volumes {
			if volume.PVCRef == nil || volume.PVCRef.Name != options.PVC || volume.PVCRef.Namespace != options.Namespace {
				continue
			}
			if volume.Time.IsZero() || now.Sub(volume.Time) > maxVolumeAge || volume.Time.After(now.Add(maxFutureSkew)) {
				return Capacity{}, errors.New("database volume sample is missing, stale or future-dated")
			}
			if volume.CapacityBytes == nil || volume.AvailableBytes == nil {
				return Capacity{}, errors.New("database volume capacity fields are missing")
			}
			capacity := Capacity{TotalBytes: *volume.CapacityBytes, AvailableBytes: *volume.AvailableBytes}
			if !FromCapacity(capacity).Known {
				return Capacity{}, errors.New("database volume capacity is invalid")
			}
			// A PVC mounted by multiple pods may appear repeatedly. Contradictory
			// samples cannot establish authoritative capacity and must remain unknown.
			if found && result != capacity {
				return Capacity{}, errors.New("database volume samples disagree")
			}
			result, found = capacity, true
		}
	}
	if !found {
		return Capacity{}, errors.New("database PVC is absent from kubelet summary")
	}
	return result, nil
}
