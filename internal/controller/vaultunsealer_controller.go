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

package controller

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/panteparak/vault-unsealer/api/v1alpha1"
	"github.com/panteparak/vault-unsealer/internal/logging"
	"github.com/panteparak/vault-unsealer/internal/metrics"
	"github.com/panteparak/vault-unsealer/internal/secrets"
	"github.com/panteparak/vault-unsealer/internal/vault"
)

// VaultUnsealerReconciler reconciles a VaultUnsealer object
type VaultUnsealerReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	SecretsLoader *secrets.Loader
}

const (
	ConditionTypeReady           = "Ready"
	ConditionTypeKeysMissing     = "KeysMissing"
	ConditionTypeVaultAPIFailure = "VaultAPIFailure"
	ConditionTypePodUnavailable  = "PodUnavailable"

	ConditionStatusTrue    = "True"
	ConditionStatusFalse   = "False"
	ConditionStatusUnknown = "Unknown"

	ReasonReconcileSuccess = "ReconcileSuccess"
	ReasonKeysMissing      = "KeysMissing"
	ReasonVaultAPIError    = "VaultAPIError"
	ReasonPodNotReady      = "PodNotReady"
	ReasonUnsealSuccess    = "UnsealSuccess"
	ReasonUnsealFailed     = "UnsealFailed"

	// Finalizer for cleanup
	VaultUnsealerFinalizer = "autounseal.vault.io/finalizer"
)

// +kubebuilder:rbac:groups=ops.autounseal.vault.io,resources=vaultunsealers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ops.autounseal.vault.io,resources=vaultunsealers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ops.autounseal.vault.io,resources=vaultunsealers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *VaultUnsealerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var vaultUnsealer opsv1alpha1.VaultUnsealer
	if err := r.Get(ctx, req.NamespacedName, &vaultUnsealer); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("VaultUnsealer resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get VaultUnsealer")
		return ctrl.Result{}, err
	}

	if r.SecretsLoader == nil {
		r.SecretsLoader = secrets.NewLoader(r.Client)
	}

	// Handle deletion
	if vaultUnsealer.DeletionTimestamp.IsZero() {
		// The object is not being deleted, ensure finalizer is present
		if !controllerutil.ContainsFinalizer(&vaultUnsealer, VaultUnsealerFinalizer) {
			controllerutil.AddFinalizer(&vaultUnsealer, VaultUnsealerFinalizer)
			return ctrl.Result{}, r.Update(ctx, &vaultUnsealer)
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(&vaultUnsealer, VaultUnsealerFinalizer) {
			// Perform cleanup
			log.Info("Performing cleanup for VaultUnsealer")

			// Clean up metrics
			r.cleanupMetrics(&vaultUnsealer)

			// Remove finalizer
			controllerutil.RemoveFinalizer(&vaultUnsealer, VaultUnsealerFinalizer)
			return ctrl.Result{}, r.Update(ctx, &vaultUnsealer)
		}
		// Finalizer removed, object will be deleted
		return ctrl.Result{}, nil
	}

	return r.reconcileVaultUnsealer(ctx, &vaultUnsealer)
}

func (r *VaultUnsealerReconciler) reconcileVaultUnsealer(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer) (ctrl.Result, error) {
	// Generate unique reconciliation ID for tracking
	reconcileID, _ := generateReconcileID()

	// Create structured logger with VaultUnsealer context
	log := logging.WithVaultUnsealer(logf.FromContext(ctx), vaultUnsealer)
	log = logging.WithReconciliation(log, reconcileID)

	log.Info("Starting reconciliation")

	// Record reconciliation metrics
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		metrics.ReconciliationDuration.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace).Observe(duration.Seconds())
		metrics.ReconciliationTotal.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace).Inc()
		log.Info("Reconciliation completed", "duration", duration.String())
	}()

	defaultInterval := 60 * time.Second
	if vaultUnsealer.Spec.Interval != nil {
		defaultInterval = vaultUnsealer.Spec.Interval.Duration
	}

	vaultUnsealer.Status.LastReconcileTime = &metav1.Time{Time: time.Now()}
	vaultUnsealer.Status.PodsChecked = []string{}
	vaultUnsealer.Status.UnsealedPods = []string{}

	pods, err := r.getVaultPods(ctx, vaultUnsealer)
	if err != nil {
		log.Error(err, "Failed to get Vault pods")
		metrics.ReconciliationErrors.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, "pod_discovery").Inc()
		r.setCondition(vaultUnsealer, ConditionTypePodUnavailable, ConditionStatusTrue, ReasonPodNotReady, err.Error())
		if updateErr := r.updateStatus(ctx, vaultUnsealer); updateErr != nil {
			log.Error(updateErr, "Failed to update status after pod discovery error")
		}
		return ctrl.Result{RequeueAfter: defaultInterval}, err
	}

	if len(pods) == 0 {
		log.Info("No Vault pods found matching label selector", "labelSelector", vaultUnsealer.Spec.VaultLabelSelector)
		r.setCondition(vaultUnsealer, ConditionTypePodUnavailable, ConditionStatusTrue, ReasonPodNotReady, "No pods found")
		if updateErr := r.updateStatus(ctx, vaultUnsealer); updateErr != nil {
			log.Error(updateErr, "Failed to update status after no pods found")
		}
		return ctrl.Result{RequeueAfter: defaultInterval}, nil
	}

	unsealKeys, err := r.SecretsLoader.LoadUnsealKeys(ctx, vaultUnsealer.Namespace, vaultUnsealer.Spec.UnsealKeysSecretRefs, vaultUnsealer.Spec.KeyThreshold)
	if err != nil {
		log.Error(err, "Failed to load unseal keys")
		metrics.ReconciliationErrors.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, "keys_loading").Inc()
		r.setCondition(vaultUnsealer, ConditionTypeKeysMissing, ConditionStatusTrue, ReasonKeysMissing, err.Error())
		if updateErr := r.updateStatus(ctx, vaultUnsealer); updateErr != nil {
			log.Error(updateErr, "Failed to update status after key loading error")
		}
		return ctrl.Result{RequeueAfter: defaultInterval}, err
	}

	log.Info("Loaded unseal keys", "keyCount", len(unsealKeys))
	metrics.UnsealKeysLoaded.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace).Set(float64(len(unsealKeys)))

	unsealedCount := 0
	for _, pod := range pods {
		vaultUnsealer.Status.PodsChecked = append(vaultUnsealer.Status.PodsChecked, pod.Name)

		if !r.isPodReady(&pod) {
			log.Info("Pod is not ready, skipping", "pod", pod.Name)
			continue
		}

		sealed, err := r.checkAndUnsealPod(ctx, &pod, vaultUnsealer, unsealKeys)
		if err != nil {
			log.Error(err, "Failed to check/unseal pod", "pod", pod.Name)
			metrics.UnsealAttempts.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, pod.Name, "failed").Inc()
			metrics.VaultConnectionStatus.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, pod.Name).Set(0)
			continue
		}

		if !sealed {
			vaultUnsealer.Status.UnsealedPods = append(vaultUnsealer.Status.UnsealedPods, pod.Name)
			unsealedCount++
			metrics.UnsealAttempts.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, pod.Name, "success").Inc()
			metrics.VaultConnectionStatus.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, pod.Name).Set(1)

			if !vaultUnsealer.Spec.Mode.HA {
				log.Info("HA mode disabled, stopping after first successful unseal", "pod", pod.Name)
				break
			}
		} else {
			metrics.VaultConnectionStatus.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, pod.Name).Set(1)
		}
	}

	// Update pod metrics
	metrics.PodsChecked.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace).Set(float64(len(vaultUnsealer.Status.PodsChecked)))
	metrics.PodsUnsealed.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace).Set(float64(unsealedCount))

	if unsealedCount > 0 {
		r.setCondition(vaultUnsealer, ConditionTypeReady, ConditionStatusTrue, ReasonReconcileSuccess, fmt.Sprintf("Successfully unsealed %d pods", unsealedCount))
	} else {
		r.setCondition(vaultUnsealer, ConditionTypeReady, ConditionStatusFalse, ReasonUnsealFailed, "No pods were successfully unsealed")
	}

	r.clearCondition(vaultUnsealer, ConditionTypeKeysMissing)
	r.clearCondition(vaultUnsealer, ConditionTypePodUnavailable)

	if err := r.updateStatus(ctx, vaultUnsealer); err != nil {
		log.Error(err, "Failed to update status")
		metrics.ReconciliationErrors.WithLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, "status_update").Inc()
		return ctrl.Result{RequeueAfter: defaultInterval}, err
	}

	log.Info("Reconciliation completed", "podsChecked", len(vaultUnsealer.Status.PodsChecked), "podsUnsealed", len(vaultUnsealer.Status.UnsealedPods))
	return ctrl.Result{RequeueAfter: defaultInterval}, nil
}

func (r *VaultUnsealerReconciler) getVaultPods(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer) ([]corev1.Pod, error) {
	selector, err := labels.Parse(vaultUnsealer.Spec.VaultLabelSelector)
	if err != nil {
		return nil, fmt.Errorf("invalid label selector: %w", err)
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, &client.ListOptions{
		Namespace:     vaultUnsealer.Namespace,
		LabelSelector: selector,
	}); err != nil {
		return nil, err
	}

	return podList.Items, nil
}

func (r *VaultUnsealerReconciler) isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	if pod.Status.PodIP == "" {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}

func (r *VaultUnsealerReconciler) checkAndUnsealPod(ctx context.Context, pod *corev1.Pod, vaultUnsealer *opsv1alpha1.VaultUnsealer, unsealKeys []string) (bool, error) {
	log := logging.WithPod(logf.FromContext(ctx), pod)

	vaultClient, err := r.createVaultClient(ctx, pod, vaultUnsealer)
	if err != nil {
		return true, fmt.Errorf("failed to create vault client: %w", err)
	}

	status, err := vaultClient.GetSealStatus(ctx)
	if err != nil {
		log.Error(err, "Failed to get seal status")
		return true, err
	}

	log.Info("Vault seal status", "sealed", status.Sealed, "progress", status.Progress, "threshold", status.T)

	if !status.Sealed {
		log.Info("Vault pod is already unsealed")
		return false, nil
	}

	for i, key := range unsealKeys {
		keyLog := logging.WithUnsealAttempt(log, pod.Name, i+1, len(unsealKeys))
		keyLog.Info("Submitting unseal key")

		unsealResp, err := vaultClient.Unseal(ctx, key)
		if err != nil {
			keyLog.Error(err, "Failed to submit unseal key")
			return true, err
		}

		keyLog.Info("Unseal key submitted successfully",
			"sealed", unsealResp.Sealed,
			"progress", unsealResp.Progress,
			"threshold", unsealResp.T)

		if !unsealResp.Sealed {
			keyLog.Info("Vault pod successfully unsealed")
			return false, nil
		}
	}

	log.Info("All keys submitted but vault still sealed", "keysSubmitted", len(unsealKeys))
	return true, nil
}

func (r *VaultUnsealerReconciler) createVaultClient(ctx context.Context, pod *corev1.Pod, vaultUnsealer *opsv1alpha1.VaultUnsealer) (*vault.Client, error) {
	vaultURL := strings.Replace(vaultUnsealer.Spec.Vault.URL, "vault.vault.svc", pod.Status.PodIP, 1)
	vaultURL = strings.Replace(vaultURL, "vault", pod.Status.PodIP, 1)

	if !strings.HasPrefix(vaultURL, "http") {
		vaultURL = "http://" + pod.Status.PodIP + ":8200"
	}

	var tlsConfig *tls.Config
	if vaultUnsealer.Spec.Vault.CABundleSecretRef != nil {
		tlsConfig, _ = r.getTLSConfig(ctx, vaultUnsealer)
	} else if vaultUnsealer.Spec.Vault.InsecureSkipVerify {
		tlsConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return vault.NewClient(vaultURL, tlsConfig)
}

func (r *VaultUnsealerReconciler) getTLSConfig(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer) (*tls.Config, error) {
	if vaultUnsealer.Spec.Vault.CABundleSecretRef == nil {
		return nil, nil
	}

	namespace := vaultUnsealer.Spec.Vault.CABundleSecretRef.Namespace
	if namespace == "" {
		namespace = vaultUnsealer.Namespace
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      vaultUnsealer.Spec.Vault.CABundleSecretRef.Name,
	}, secret); err != nil {
		return nil, err
	}

	caData, ok := secret.Data[vaultUnsealer.Spec.Vault.CABundleSecretRef.Key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in CA bundle secret", vaultUnsealer.Spec.Vault.CABundleSecretRef.Key)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caData) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return &tls.Config{RootCAs: caCertPool}, nil
}

func (r *VaultUnsealerReconciler) setCondition(vaultUnsealer *opsv1alpha1.VaultUnsealer, condType, status, reason, message string) {
	condition := opsv1alpha1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  reason,
		Message: message,
	}

	for i, existingCondition := range vaultUnsealer.Status.Conditions {
		if existingCondition.Type == condType {
			vaultUnsealer.Status.Conditions[i] = condition
			return
		}
	}

	vaultUnsealer.Status.Conditions = append(vaultUnsealer.Status.Conditions, condition)
}

func (r *VaultUnsealerReconciler) clearCondition(vaultUnsealer *opsv1alpha1.VaultUnsealer, condType string) {
	for i, condition := range vaultUnsealer.Status.Conditions {
		if condition.Type == condType {
			vaultUnsealer.Status.Conditions = append(
				vaultUnsealer.Status.Conditions[:i],
				vaultUnsealer.Status.Conditions[i+1:]...,
			)
			return
		}
	}
}

func (r *VaultUnsealerReconciler) updateStatus(ctx context.Context, vaultUnsealer *opsv1alpha1.VaultUnsealer) error {
	return r.Status().Update(ctx, vaultUnsealer)
}

// generateReconcileID creates a unique identifier for tracking reconciliation operations
func generateReconcileID() (string, error) {
	bytes := make([]byte, 4)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func (r *VaultUnsealerReconciler) cleanupMetrics(vaultUnsealer *opsv1alpha1.VaultUnsealer) {
	// Clean up Prometheus metrics to prevent memory leaks
	metrics.ReconciliationTotal.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace)
	metrics.ReconciliationErrors.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, "pod_discovery")
	metrics.ReconciliationErrors.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, "keys_loading")
	metrics.ReconciliationErrors.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, "status_update")
	metrics.PodsUnsealed.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace)
	metrics.PodsChecked.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace)
	metrics.UnsealKeysLoaded.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace)
	metrics.ReconciliationDuration.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace)

	// Clean up pod-specific metrics for all pods that were tracked
	if len(vaultUnsealer.Status.PodsChecked) > 0 {
		for _, podName := range vaultUnsealer.Status.PodsChecked {
			metrics.UnsealAttempts.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, podName, "success")
			metrics.UnsealAttempts.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, podName, "failed")
			metrics.VaultConnectionStatus.DeleteLabelValues(vaultUnsealer.Name, vaultUnsealer.Namespace, podName)
		}
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *VaultUnsealerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.VaultUnsealer{}).
		Named("vaultunsealer").
		Complete(r)
}
