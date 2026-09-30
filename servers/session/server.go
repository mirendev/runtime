package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	coreutil "miren.dev/runtime/api/core"
	core "miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	shared "miren.dev/runtime/api/session"
	sessionapi "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/appspec"
	"miren.dev/runtime/pkg/cond"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

type Server struct {
	Log *slog.Logger
	EAC *entityserver_v1alpha.EntityAccessClient
}

func NewServer(log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) *Server {
	return &Server{Log: log, EAC: eac}
}

var _ sessionapi.Sessions = (*Server)(nil)

func invalid(message string) error {
	return cond.ErrValidationFailure{Category: "session", Message: message}
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/:")
}

func (s *Server) app(ctx context.Context, name string) (*core.App, error) {
	if !rpc.AllowApp(ctx, name) {
		return nil, rpc.AppAccessError(ctx, name)
	}
	if !validName(name) {
		return nil, invalid("invalid app name")
	}
	resp, err := s.EAC.Get(ctx, "app/"+name)
	if err != nil {
		return nil, err
	}
	var app core.App
	app.Decode(resp.Entity().Entity())
	return &app, nil
}

func sessionID(app, name string) (entity.Id, error) {
	if !validName(name) {
		return "", invalid("invalid Session name")
	}
	return entity.Id("session/" + app + "/" + name), nil
}

func (s *Server) read(ctx context.Context, app *core.App, name string) (*sessionapi.Session, error) {
	id, err := sessionID(strings.TrimPrefix(app.ID.String(), "app/"), name)
	if err != nil {
		return nil, err
	}
	resp, err := s.EAC.Get(ctx, id.String())
	if err != nil {
		return nil, err
	}
	var current sessionapi.Session
	current.Decode(resp.Entity().Entity())
	if !entity.Is(resp.Entity().Entity(), sessionapi.KindSession) || current.App != app.ID {
		return nil, cond.NotFound("session", id)
	}
	return &current, nil
}

func info(current *sessionapi.Session) *sessionapi.SessionInfo {
	result := &sessionapi.SessionInfo{}
	result.SetId(current.ID.String())
	result.SetApp(current.App.String())
	result.SetVersion(current.Version.String())
	result.SetService(current.Service)
	result.SetGroup(current.Group)
	result.SetMaxSessionsPerSandbox(current.MaxSessionsPerSandbox)
	result.SetDesiredState(strings.TrimPrefix(string(current.DesiredState), "desired_state."))
	result.SetPhase(strings.TrimPrefix(string(current.Phase), "phase."))
	result.SetSandbox(current.Sandbox.String())
	result.SetFailure(current.Failure)
	return result
}

func (s *Server) Create(ctx context.Context, state *sessionapi.SessionsCreate) error {
	args := state.Args()
	app, err := s.app(ctx, args.App())
	if err != nil {
		return err
	}
	capacity := args.MaxSessionsPerSandbox()
	if !args.HasMaxSessionsPerSandbox() {
		capacity = 1
	}
	if capacity < 1 {
		return invalid("max_sessions_per_sandbox must be greater than zero")
	}
	name := args.Name()
	if name == "" {
		var random [6]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		name = hex.EncodeToString(random[:])
	}
	id, err := sessionID(args.App(), name)
	if err != nil {
		return err
	}
	if _, err := s.EAC.Get(ctx, shared.BindingID(id).String()); err == nil {
		return invalid("previous shared Session cleanup is still pending")
	} else if !errors.Is(err, cond.ErrNotFound{}) {
		return err
	}
	if app.ActiveVersion == "" {
		return invalid("app has no active version")
	}
	resp, err := s.EAC.Get(ctx, app.ActiveVersion.String())
	if err != nil {
		return err
	}
	var version core.AppVersion
	version.Decode(resp.Entity().Entity())
	cfg, err := coreutil.ResolveRuntimeConfig(ctx, s.EAC, &version)
	if err != nil {
		return err
	}
	service := args.Service()
	if service == "" {
		service = "web"
	}
	found := false
	for _, candidate := range cfg.Services {
		if candidate.Name == service {
			found = true
			break
		}
	}
	if !found {
		return invalid(fmt.Sprintf("service %q is not defined on app %q", service, args.App()))
	}
	spec, err := appspec.Build(s.Log, appspec.Options{
		AppID: app.ID, AppName: args.App(), Version: &version,
		Config: cfg, Service: service, Image: version.ImageUrl, SkipDisks: true,
	})
	if err != nil {
		return err
	}
	if capacity > 1 {
		for _, volume := range spec.Volume {
			if volume.Provider == "miren" {
				return invalid("shared Sessions cannot mount per-Session disks")
			}
		}
	}
	// The two schema components describe the same execution spec.
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	var sessionSpec sessionapi.SandboxSpec
	if err := json.Unmarshal(data, &sessionSpec); err != nil {
		return err
	}
	current := &sessionapi.Session{ID: id, App: app.ID, Version: version.ID, Service: service, Group: args.Group(),
		Spec: sessionSpec, MaxSessionsPerSandbox: capacity,
		DesiredState: sessionapi.RUNNING}
	if _, err := s.EAC.Create(ctx, entity.New(entity.DBId, id, current.Encode).Attrs()); err != nil {
		return err
	}
	state.Results().SetSession(info(current))
	return nil
}

func (s *Server) List(ctx context.Context, state *sessionapi.SessionsList) error {
	app, err := s.app(ctx, state.Args().App())
	if err != nil {
		return err
	}
	resp, err := s.EAC.List(ctx, entity.Ref(sessionapi.SessionAppId, app.ID))
	if err != nil {
		return err
	}
	items := make([]*sessionapi.SessionInfo, 0, len(resp.Values()))
	for _, value := range resp.Values() {
		var current sessionapi.Session
		current.Decode(value.Entity())
		if current.App == app.ID && entity.Is(value.Entity(), sessionapi.KindSession) {
			items = append(items, info(&current))
		}
	}
	slices.SortFunc(items, func(a, b *sessionapi.SessionInfo) int { return strings.Compare(a.Id(), b.Id()) })
	state.Results().SetSessions(items)
	return nil
}

func (s *Server) Get(ctx context.Context, state *sessionapi.SessionsGet) error {
	app, err := s.app(ctx, state.Args().App())
	if err != nil {
		return err
	}
	current, err := s.read(ctx, app, state.Args().Name())
	if err != nil {
		return err
	}
	state.Results().SetSession(info(current))
	return nil
}

func (s *Server) SetDesiredState(ctx context.Context, state *sessionapi.SessionsSetDesiredState) error {
	app, err := s.app(ctx, state.Args().App())
	if err != nil {
		return err
	}
	current, err := s.read(ctx, app, state.Args().Name())
	if err != nil {
		return err
	}
	var desired sessionapi.SessionDesiredState
	switch state.Args().DesiredState() {
	case "running":
		desired = sessionapi.RUNNING
	case "suspended":
		desired = sessionapi.SUSPENDED
	default:
		return invalid("desired_state must be running or suspended")
	}
	if _, err := s.EAC.Patch(ctx, entity.New(entity.DBId, current.ID,
		(&sessionapi.Session{DesiredState: desired}).Encode).Attrs(), 0); err != nil {
		return err
	}
	current.DesiredState = desired
	state.Results().SetSession(info(current))
	return nil
}

func (s *Server) Delete(ctx context.Context, state *sessionapi.SessionsDelete) error {
	app, err := s.app(ctx, state.Args().App())
	if err != nil {
		return err
	}
	current, err := s.read(ctx, app, state.Args().Name())
	if err != nil {
		return err
	}
	_, err = s.EAC.Delete(ctx, current.ID.String())
	return err
}
