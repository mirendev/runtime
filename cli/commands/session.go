package commands

import (
	"fmt"
	"strings"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	session "miren.dev/runtime/api/session/session_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/ui"
)

func SessionCreate(ctx *Context, opts struct {
	AppCentric
	Name        string `long:"name" description:"Stable name for the Session; generated if omitted"`
	Service     string `short:"s" long:"service" description:"Service to run" default:"web"`
	Group       string `long:"group" description:"Optional opaque sharing key within the app and service"`
	MaxSessions int64  `long:"max-sessions-per-sandbox" description:"Shared host capacity (greater than one enables sharing)" default:"1"`
}) error {
	client, err := ctx.RPCClient("dev.miren.runtime/sessions")
	if err != nil {
		return err
	}
	resp, err := session.NewSessionsClient(client).Create(ctx, opts.App, opts.Name, opts.Service, opts.Group,
		opts.MaxSessions)
	if err != nil {
		return err
	}
	ctx.Printf("%s\n", resp.Session().Id())
	return nil
}

type sessionInfo struct {
	ID           string `json:"id"`
	App          string `json:"app"`
	Version      string `json:"version"`
	Service      string `json:"service"`
	Group        string `json:"group,omitempty"`
	MaxSessions  int64  `json:"max_sessions_per_sandbox"`
	DesiredState string `json:"desired_state"`
	Phase        string `json:"phase"`
	Sandbox      string `json:"sandbox,omitempty"`
	Failure      string `json:"failure,omitempty"`
}

func sessionSummary(s *session.Session) sessionInfo {
	return sessionInfo{
		ID: s.ID.String(), App: s.App.String(), Version: s.Version.String(),
		Service: s.Service, Group: s.Group, MaxSessions: s.MaxSessionsPerSandbox,
		DesiredState: strings.TrimPrefix(string(s.DesiredState), "desired_state."),
		Phase:        strings.TrimPrefix(string(s.Phase), "phase."), Sandbox: s.Sandbox.String(), Failure: s.Failure,
	}
}

func SessionList(ctx *Context, opts struct {
	ConfigCentric
	FormatOptions
}) error {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return err
	}
	eac := entityserver_v1alpha.NewEntityAccessClient(client)
	resp, err := eac.List(ctx, entity.Ref(entity.EntityKind, session.KindSession))
	if err != nil {
		return err
	}
	items := make([]sessionInfo, 0, len(resp.Values()))
	for _, value := range resp.Values() {
		var s session.Session
		s.Decode(value.Entity())
		items = append(items, sessionSummary(&s))
	}
	if opts.IsJSON() {
		return PrintJSON(items)
	}
	if len(items) == 0 {
		ctx.Printf("No Sessions found\n")
		return nil
	}
	var rows []ui.Row
	for _, item := range items {
		rows = append(rows, ui.Row{item.ID, item.App, item.Service, item.Phase, item.Sandbox})
	}
	ctx.Printf("%s\n", ui.NewTable(ui.WithColumns(ui.AutoSizeColumns(
		[]string{"ID", "APP", "SERVICE", "PHASE", "SANDBOX"}, rows, ui.Columns().NoTruncate(0))), ui.WithRows(rows)).Render())
	return nil
}

func SessionGet(ctx *Context, opts struct {
	ConfigCentric
	FormatOptions
	ID string `position:"0" usage:"Session ID" required:"true"`
}) error {
	eac, err := sessionEntityClient(ctx)
	if err != nil {
		return err
	}
	s, err := readSession(ctx, eac, opts.ID)
	if err != nil {
		return err
	}
	item := sessionSummary(s)
	if opts.IsJSON() {
		return PrintJSON(item)
	}
	ctx.Printf("ID: %s\nApp: %s\nService: %s\nGroup: %s\nDesired: %s\nPhase: %s\nSandbox: %s\n", item.ID, item.App, item.Service, item.Group, item.DesiredState, item.Phase, item.Sandbox)
	return nil
}

func SessionDelete(ctx *Context, opts struct {
	ConfigCentric
	ID string `position:"0" usage:"Session ID" required:"true"`
}) error {
	eac, err := sessionEntityClient(ctx)
	if err != nil {
		return err
	}
	if _, err := readSession(ctx, eac, opts.ID); err != nil {
		return err
	}
	if _, err := eac.Delete(ctx, opts.ID); err != nil {
		return err
	}
	ctx.Printf("Deleted %s\n", opts.ID)
	return nil
}

func SessionSuspend(ctx *Context, opts struct {
	ConfigCentric
	ID string `position:"0" usage:"Session ID" required:"true"`
}) error {
	return setSessionState(ctx, opts.ID, session.SUSPENDED)
}

func SessionResume(ctx *Context, opts struct {
	ConfigCentric
	ID string `position:"0" usage:"Session ID" required:"true"`
}) error {
	return setSessionState(ctx, opts.ID, session.RUNNING)
}

func setSessionState(ctx *Context, id string, state session.SessionDesiredState) error {
	eac, err := sessionEntityClient(ctx)
	if err != nil {
		return err
	}
	if _, err := readSession(ctx, eac, id); err != nil {
		return err
	}
	_, err = eac.Patch(ctx, entity.New(entity.DBId, entity.Id(id),
		(&session.Session{DesiredState: state}).Encode).Attrs(), 0)
	if err != nil {
		return err
	}
	ctx.Printf("Requested %s for %s\n", strings.TrimPrefix(string(state), "desired_state."), id)
	return nil
}

func sessionEntityClient(ctx *Context) (*entityserver_v1alpha.EntityAccessClient, error) {
	client, err := ctx.RPCClient("entities")
	if err != nil {
		return nil, err
	}
	return entityserver_v1alpha.NewEntityAccessClient(client), nil
}

func readSession(ctx *Context, eac *entityserver_v1alpha.EntityAccessClient, id string) (*session.Session, error) {
	resp, err := eac.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	var s session.Session
	s.Decode(resp.Entity().Entity())
	if !entity.Is(resp.Entity().Entity(), session.KindSession) {
		return nil, fmt.Errorf("%s is not a Session", id)
	}
	return &s, nil
}
