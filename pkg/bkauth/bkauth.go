package bkauth

import (
	"context"
	"log/slog"

	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth"
	"google.golang.org/grpc"
)

type Auth struct {
	Log *slog.Logger
	Sub auth.AuthServer
}

var (
	_ auth.AuthServer    = &Auth{}
	_ session.Attachable = &Auth{}
)

func (a *Auth) Register(server *grpc.Server) {
	auth.RegisterAuthServer(server, a)
}

func (a *Auth) Credentials(ctx context.Context, req *auth.CredentialsRequest) (*auth.CredentialsResponse, error) {
	a.Log.Info("received credentials request", "req", req)
	return a.Sub.Credentials(ctx, req)
}

func (a *Auth) FetchToken(ctx context.Context, req *auth.FetchTokenRequest) (*auth.FetchTokenResponse, error) {
	a.Log.Info("received fetch token request", "req", req)
	return a.Sub.FetchToken(ctx, req)
}

func (a *Auth) GetTokenAuthority(ctx context.Context, req *auth.GetTokenAuthorityRequest) (*auth.GetTokenAuthorityResponse, error) {
	a.Log.Info("received get token authority request", "req", req)
	return a.Sub.GetTokenAuthority(ctx, req)
}

func (a *Auth) VerifyTokenAuthority(ctx context.Context, req *auth.VerifyTokenAuthorityRequest) (*auth.VerifyTokenAuthorityResponse, error) {
	a.Log.Info("received verify token authority request", "req", req)
	return a.Sub.VerifyTokenAuthority(ctx, req)
}
