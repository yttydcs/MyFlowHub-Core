package login_server

import (
	"context"

	core "MyFlowHub-Core/internal/core"
)

// ProcessWrapper intercepts parent connect/close events to trigger registrar while delegating to inner process.
type ProcessWrapper struct {
	inner          core.IProcess
	registrar      *Registrar
	serverProvider func() core.IServer
}

func NewProcessWrapper(inner core.IProcess, registrar *Registrar) *ProcessWrapper {
	return &ProcessWrapper{inner: inner, registrar: registrar}
}

func (p *ProcessWrapper) SetServerProvider(fn func() core.IServer) {
	p.serverProvider = fn
}

func (p *ProcessWrapper) OnListen(conn core.IConnection) {
	if p.registrar != nil && isParentConn(conn) {
		var srv core.IServer
		if p.serverProvider != nil {
			srv = p.serverProvider()
		}
		p.registrar.OnParentConnected(context.Background(), srv, conn)
	}
	if p.inner != nil {
		p.inner.OnListen(conn)
	}
}

func (p *ProcessWrapper) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if p.inner != nil {
		p.inner.OnReceive(ctx, conn, hdr, payload)
	}
}

func (p *ProcessWrapper) OnSend(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) error {
	if p.inner != nil {
		return p.inner.OnSend(ctx, conn, hdr, payload)
	}
	return nil
}

func (p *ProcessWrapper) OnClose(conn core.IConnection) {
	if p.registrar != nil && isParentConn(conn) {
		p.registrar.OnParentClosed(conn.ID())
	}
	if p.inner != nil {
		p.inner.OnClose(conn)
	}
}

func isParentConn(c core.IConnection) bool {
	if c == nil {
		return false
	}
	if role, ok := c.GetMeta(core.MetaRoleKey); ok {
		if s, ok2 := role.(string); ok2 && s == core.RoleParent {
			return true
		}
	}
	return false
}
