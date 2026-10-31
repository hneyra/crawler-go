package httpapi

import (
	"context"
	"time"
)

// Pinger = db health. pgxpool.Pool fits.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Ops = health endpoints.
type Ops struct {
	DB      Pinger
	Version string
}

func (o *Ops) GetHealthz(context.Context, GetHealthzRequestObject) (GetHealthzResponseObject, error) {
	v := o.Version
	return GetHealthz200JSONResponse{Status: Ok, Version: &v}, nil
}

func (o *Ops) GetReadyz(ctx context.Context, _ GetReadyzRequestObject) (GetReadyzResponseObject, error) {
	// short: probe answers before kubelet timeout.
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if o.DB == nil || o.DB.Ping(ctx) != nil {
		return GetReadyz503ApplicationProblemPlusJSONResponse{Type: "about:blank", Title: "Base de datos no disponible", Status: 503}, nil
	}
	v := o.Version
	return GetReadyz200JSONResponse{Status: Ok, Version: &v}, nil
}
