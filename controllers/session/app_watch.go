package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	coreutil "miren.dev/runtime/api/core"
	core "miren.dev/runtime/api/core/core_v1alpha"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/entity"
)

// AppWatchController updates Session workload specs when an app deploys.
// The Session controller owns the subsequent sandbox rollout.
type AppWatchController struct{ Sessions *Controller }

func serviceMissingFailure(app entity.Id, service string) string {
	return fmt.Sprintf("app %s no longer defines Session service %s", app, service)
}

func (w *AppWatchController) Init(context.Context) error { return nil }

func (w *AppWatchController) Create(ctx context.Context, app *core.App, _ *entity.Meta) error {
	return w.Update(ctx, app, nil)
}

func (w *AppWatchController) Update(ctx context.Context, app *core.App, _ *entity.Meta) error {
	appResp, err := w.Sessions.EAC.Get(ctx, app.ID.String())
	if err != nil {
		return err
	}
	var current core.App
	current.Decode(appResp.Entity().Entity())
	if current.ActiveVersion == "" {
		return nil
	}
	versionResp, err := w.Sessions.EAC.Get(ctx, current.ActiveVersion.String())
	if err != nil {
		return err
	}
	var version core.AppVersion
	version.Decode(versionResp.Entity().Entity())
	cfg, err := coreutil.ResolveRuntimeConfig(ctx, w.Sessions.EAC, &version)
	if err != nil {
		return err
	}
	var metadata core.Metadata
	metadata.Decode(appResp.Entity().Entity())
	sessions, err := w.Sessions.EAC.List(ctx, entity.Ref(sessionapi.SessionAppId, app.ID))
	if err != nil {
		return err
	}
	for _, value := range sessions.Values() {
		var s sessionapi.Session
		s.Decode(value.Entity())
		if s.App != app.ID || s.Version == version.ID {
			continue
		}
		found := false
		for _, service := range cfg.Services {
			found = found || service.Name == s.Service
		}
		if !found {
			cause := serviceMissingFailure(app.ID, s.Service)
			if err := w.Sessions.patch(ctx, s.ID, &sessionapi.Session{Phase: sessionapi.FAILED, Failure: cause, LastTransition: time.Now()}); err != nil {
				return err
			}
			w.Sessions.Log.Warn("session service removed during deploy", "session", s.ID, "error", cause)
			continue
		}
		spec, err := appspec.Build(w.Sessions.Log, appspec.Options{
			AppID: app.ID, AppName: metadata.Name, Version: &version,
			Config: cfg, Service: s.Service, Image: version.ImageUrl, SkipDisks: true,
		})
		if err != nil {
			return err
		}
		b, err := json.Marshal(spec)
		if err != nil {
			return err
		}
		var sessionSpec sessionapi.SandboxSpec
		if err := json.Unmarshal(b, &sessionSpec); err != nil {
			return err
		}
		e := entity.New(value.Attrs())
		e.Remove(sessionapi.SessionSpecId)
		e.Remove(sessionapi.SessionVersionId)
		if s.Failure == serviceMissingFailure(app.ID, s.Service) {
			e.Remove(sessionapi.SessionFailureId)
		}
		for _, attr := range (&sessionapi.Session{Spec: sessionSpec, Version: version.ID}).Encode() {
			if attr.ID == sessionapi.SessionSpecId || attr.ID == sessionapi.SessionVersionId {
				e.Set(attr)
			}
		}
		if _, err := w.Sessions.EAC.Replace(ctx, e.Attrs(), value.Revision()); err != nil {
			return err
		}
	}
	return nil
}

func (w *AppWatchController) Delete(context.Context, entity.Id, *core.App) error { return nil }
