/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// ReconciliationTotal tracks total number of reconciliation attempts
	ReconciliationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_unsealer_reconciliation_total",
			Help: "Total number of reconciliation attempts",
		},
		[]string{"vaultunsealer", "namespace"},
	)

	// ReconciliationErrors tracks reconciliation errors
	ReconciliationErrors = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_unsealer_reconciliation_errors_total",
			Help: "Total number of reconciliation errors",
		},
		[]string{"vaultunsealer", "namespace", "error_type"},
	)

	// UnsealAttempts tracks unseal attempts per pod
	UnsealAttempts = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vault_unsealer_unseal_attempts_total",
			Help: "Total number of unseal attempts",
		},
		[]string{"vaultunsealer", "namespace", "pod", "status"},
	)

	// PodsUnsealed tracks number of successfully unsealed pods
	PodsUnsealed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vault_unsealer_pods_unsealed",
			Help: "Number of pods successfully unsealed",
		},
		[]string{"vaultunsealer", "namespace"},
	)

	// PodsChecked tracks number of pods checked
	PodsChecked = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vault_unsealer_pods_checked",
			Help: "Number of pods checked for sealing status",
		},
		[]string{"vaultunsealer", "namespace"},
	)

	// UnsealKeysLoaded tracks number of unseal keys loaded
	UnsealKeysLoaded = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vault_unsealer_unseal_keys_loaded",
			Help: "Number of unseal keys loaded from secrets",
		},
		[]string{"vaultunsealer", "namespace"},
	)

	// ReconciliationDuration tracks reconciliation duration
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vault_unsealer_reconciliation_duration_seconds",
			Help:    "Time taken to complete reconciliation",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"vaultunsealer", "namespace"},
	)

	// VaultConnectionStatus tracks Vault connection health
	VaultConnectionStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "vault_unsealer_vault_connection_status",
			Help: "Vault connection status (1=healthy, 0=unhealthy)",
		},
		[]string{"vaultunsealer", "namespace", "pod"},
	)
)

func init() {
	// Register metrics with controller-runtime's registry
	metrics.Registry.MustRegister(
		ReconciliationTotal,
		ReconciliationErrors,
		UnsealAttempts,
		PodsUnsealed,
		PodsChecked,
		UnsealKeysLoaded,
		ReconciliationDuration,
		VaultConnectionStatus,
	)
}
