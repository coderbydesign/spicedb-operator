package handlers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/authzed/spicedb-operator/pkg/libctrl"
	"github.com/authzed/spicedb-operator/pkg/libctrl/handler"
	"github.com/authzed/spicedb-operator/pkg/metadata"
)

const (
	EventRunningMigrations = "RunningMigrations"

	HandlerDeploymentKey        handler.Key = "deploymentChain"
	HandlerMigrationRunKey      handler.Key = "runMigration"
	HandlerWaitForMigrationsKey handler.Key = "waitForMigrationChain"
)

// TODO: could be generalized as some sort of "Hash Handoff" flow
type MigrationCheckHandler struct {
	recorder record.EventRecorder

	nextMigrationRunHandler handler.ContextHandler
	nextWaitForJobHandler   handler.ContextHandler
	nextDeploymentHandler   handler.ContextHandler
}

func NewMigrationCheckHandler(recorder record.EventRecorder, next handler.Handlers) handler.Handler {
	return handler.NewHandler(&MigrationCheckHandler{
		recorder:                recorder,
		nextMigrationRunHandler: HandlerMigrationRunKey.MustFind(next),
		nextWaitForJobHandler:   HandlerWaitForMigrationsKey.MustFind(next),
		nextDeploymentHandler:   HandlerDeploymentKey.MustFind(next),
	}, "checkMigrations")
}

func (m *MigrationCheckHandler) Handle(ctx context.Context) {
	deployments := CtxDeployments.MustValue(ctx)
	jobs := CtxJobs.MustValue(ctx)
	migrationHash := CtxMigrationHash.MustValue(ctx)

	hasJob := false
	hasDeployment := false
	for _, d := range deployments {
		if d.Annotations != nil && libctrl.SecureHashEqual(d.Annotations[metadata.SpiceDBMigrationRequirementsKey], migrationHash) {
			hasDeployment = true
			break
		}
	}
	for _, j := range jobs {
		if j.Annotations != nil && libctrl.SecureHashEqual(j.Annotations[metadata.SpiceDBMigrationRequirementsKey], migrationHash) {
			hasJob = true
			ctx = CtxCurrentMigrationJob.WithValue(ctx, j)
			break
		}
	}

	// don't handle migrations at all if `skipMigrations` is set
	if CtxConfig.MustValue(ctx).SkipMigrations {
		m.nextDeploymentHandler.Handle(ctx)
		return
	}

	// if there's no job and no (updated) deployment, create the job
	if !hasDeployment && !hasJob {
		m.recorder.Eventf(CtxClusterStatus.MustValue(ctx), corev1.EventTypeNormal, EventRunningMigrations, "Running migration job for %s", CtxConfig.MustValue(ctx).TargetSpiceDBImage)
		m.nextMigrationRunHandler.Handle(ctx)
		return
	}

	// if there's a job but no (updated) deployment, wait for the job
	if hasJob && !hasDeployment {
		m.nextWaitForJobHandler.Handle(ctx)
		return
	}

	// if the deployment is up to date, continue
	m.nextDeploymentHandler.Handle(ctx)
}
