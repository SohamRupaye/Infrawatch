package healing

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/SohamRupaye/infrawatch/apps/engine/config"
	"go.uber.org/zap"
)

// KubectlHealer performs rolling restarts of Kubernetes Deployments.
// It avoids pulling in the full k8s client-go SDK by shelling out to kubectl,
// which keeps the binary small and works everywhere kubectl is installed.
type KubectlHealer struct {
	kubeconfigPath string
	logger         *zap.Logger
}

// NewKubectlHealer creates a KubectlHealer.
func NewKubectlHealer(kubeconfigPath string, logger *zap.Logger) *KubectlHealer {
	return &KubectlHealer{kubeconfigPath: kubeconfigPath, logger: logger}
}

// Restart runs `kubectl rollout restart deployment/<name> -n <namespace>`.
// It waits for the command to exit (non-streaming), respecting ctx deadline.
func (k *KubectlHealer) Restart(ctx context.Context, svc config.ServiceConfig) HealResult {
	if svc.Deployment == "" {
		return HealResult{
			Action:  "kubectl_restart",
			Success: false,
			Error:   fmt.Errorf("kubectl_restart: deployment is not set for service %s", svc.Name),
		}
	}
	if svc.Namespace == "" {
		return HealResult{
			Action:  "kubectl_restart",
			Success: false,
			Error:   fmt.Errorf("kubectl_restart: namespace is not set for service %s", svc.Name),
		}
	}

	args := []string{
		"rollout", "restart",
		fmt.Sprintf("deployment/%s", svc.Deployment),
		"-n", svc.Namespace,
	}
	if k.kubeconfigPath != "" {
		args = append([]string{"--kubeconfig", k.kubeconfigPath}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	k.logger.Sugar().Infow("running kubectl rollout restart",
		"deployment", svc.Deployment,
		"namespace", svc.Namespace,
	)

	if err := cmd.Run(); err != nil {
		return HealResult{
			Action:  "kubectl_restart",
			Success: false,
			Error: fmt.Errorf("kubectl_restart: command failed: %w (stderr: %s)",
				err, stderr.String()),
		}
	}

	k.logger.Sugar().Infow("kubectl rollout restart succeeded",
		"deployment", svc.Deployment,
		"namespace", svc.Namespace,
		"output", stdout.String(),
	)

	return HealResult{
		Action:    "kubectl_restart",
		Success:   true,
		Timestamp: time.Now(),
	}
}

// RolloutStatus returns the current rollout status string for a deployment.
// Used by the API to surface K8s rollout progress without full client-go.
func (k *KubectlHealer) RolloutStatus(ctx context.Context, deployment, namespace string) (string, error) {
	args := []string{"rollout", "status", fmt.Sprintf("deployment/%s", deployment), "-n", namespace, "--timeout=30s"}
	if k.kubeconfigPath != "" {
		args = append([]string{"--kubeconfig", k.kubeconfigPath}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rollout status: %w — %s", err, out.String())
	}
	return out.String(), nil
}
