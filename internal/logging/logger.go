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

package logging

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
)

// WithVaultUnsealer adds structured logging fields for a VaultUnsealer resource
func WithVaultUnsealer(logger logr.Logger, vu *opsv1alpha1.VaultUnsealer) logr.Logger {
	return logger.WithValues(
		"vaultunsealer", vu.Name,
		"namespace", vu.Namespace,
		"generation", vu.Generation,
		"resourceVersion", vu.ResourceVersion,
	)
}

// WithPod adds structured logging fields for a Pod resource
func WithPod(logger logr.Logger, pod *corev1.Pod) logr.Logger {
	return logger.WithValues(
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"podIP", pod.Status.PodIP,
		"phase", pod.Status.Phase,
	)
}

// WithSecret adds structured logging fields for a Secret reference
func WithSecret(logger logr.Logger, secretRef opsv1alpha1.SecretRef, namespace string) logr.Logger {
	secretNamespace := secretRef.Namespace
	if secretNamespace == "" {
		secretNamespace = namespace
	}

	return logger.WithValues(
		"secret", secretRef.Name,
		"secretNamespace", secretNamespace,
		"secretKey", secretRef.Key,
	)
}

// WithError adds structured error information
func WithError(logger logr.Logger, err error, operation string) logr.Logger {
	return logger.WithValues(
		"error", err.Error(),
		"operation", operation,
	)
}

// WithReconciliation adds reconciliation context
func WithReconciliation(logger logr.Logger, reconcileID string) logr.Logger {
	return logger.WithValues(
		"reconcileID", reconcileID,
	)
}

// WithUnsealAttempt adds unseal attempt context
func WithUnsealAttempt(logger logr.Logger, podName string, keyIndex int, totalKeys int) logr.Logger {
	return logger.WithValues(
		"pod", podName,
		"keyIndex", keyIndex,
		"totalKeys", totalKeys,
	)
}

// WithVaultConnection adds Vault connection context
func WithVaultConnection(logger logr.Logger, vaultURL string, tlsEnabled bool) logr.Logger {
	return logger.WithValues(
		"vaultURL", vaultURL,
		"tlsEnabled", tlsEnabled,
	)
}

// WithMetrics adds metrics operation context
func WithMetrics(logger logr.Logger, metricName string, operation string) logr.Logger {
	return logger.WithValues(
		"metricName", metricName,
		"metricsOperation", operation,
	)
}

// LogLevel represents different log levels
type LogLevel int

const (
	Debug LogLevel = iota
	Info
	Warning
	Error
)

// ShouldLog determines if a message should be logged based on level
func (l LogLevel) ShouldLog(configuredLevel LogLevel) bool {
	return l >= configuredLevel
}

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case Debug:
		return "debug"
	case Info:
		return "info"
	case Warning:
		return "warning"
	case Error:
		return "error"
	default:
		return "unknown"
	}
}
