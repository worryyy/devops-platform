package report

import (
	"context"
	"fmt"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// JobSpec is the input for one report-runner Job.
type JobSpec struct {
	RunID     int64
	Kind      string
	Period    string // e.g. 2026-W38
	Start     time.Time
	End       time.Time
	Image     string
	Env       map[string]string
	Namespace string
}

// JobLauncher spawns report-runner Jobs. The k8s implementation is wired in
// app setup; tests use a recording fake.
type JobLauncher interface {
	Launch(ctx context.Context, spec JobSpec) (jobName string, err error)
}

// K8sJobLauncher creates Jobs in the platform namespace via the in-cluster
// config.
type K8sJobLauncher struct {
	clientset *kubernetes.Clientset
}

func NewK8sJobLauncher() (*K8sJobLauncher, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes clientset: %w", err)
	}
	return &K8sJobLauncher{clientset: clientset}, nil
}

// Launch creates a one-shot report-runner Job. backoffLimit 0: a failed run
// is recorded as failed by the runner itself (or left running for a manual
// retry) instead of re-sending Feishu summaries.
func (l *K8sJobLauncher) Launch(ctx context.Context, spec JobSpec) (string, error) {
	if spec.Namespace == "" {
		spec.Namespace = "platform"
	}
	// Period labels carry an uppercase W (2026-W38); k8s names must be lowercase.
	name := fmt.Sprintf("report-%s-%s-%d", spec.Kind, strings.ToLower(spec.Period), spec.RunID)
	labels := map[string]string{
		"app.kubernetes.io/name":     "report-runner",
		"app.kubernetes.io/part-of":  "devops-platform",
		"platform.devops/report-run": fmt.Sprintf("%d", spec.RunID),
	}
	env := make([]corev1.EnvVar, 0, len(spec.Env)+3)
	for key, value := range spec.Env {
		env = append(env, corev1.EnvVar{Name: key, Value: value})
	}
	env = append(env,
		corev1.EnvVar{Name: "REPORT_RUN_ID", Value: fmt.Sprintf("%d", spec.RunID)},
		corev1.EnvVar{Name: "PERIOD", Value: spec.Period},
		corev1.EnvVar{Name: "REPORT_KIND", Value: spec.Kind},
	)
	one := int32(1)
	zero := int32(0)
	ttl := int32(3600)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &zero,
			TTLSecondsAfterFinished: &ttl,
			Parallelism:             &one,
			Completions:             &one,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					// The runner image lives in the private ACR registry.
					ImagePullSecrets: []corev1.LocalObjectReference{{Name: "tcr-secret"}},
					Containers: []corev1.Container{{
						Name:  "runner",
						Image: spec.Image,
						Env:   env,
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					}},
					NodeSelector: map[string]string{"platform-role": "light"},
				},
			},
		},
	}
	_, err := l.clientset.BatchV1().Jobs(spec.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("create report job: %w", err)
	}
	return name, nil
}
