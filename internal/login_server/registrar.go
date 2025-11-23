package login_server

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

const actionRootServerRegister = "root_server_register"

type rootRegisterData struct {
	Token  string `json:"token"`
	NodeID uint32 `json:"node_id,omitempty"`
}

// Registrar sends root_server_register once connected to the root (no parent).
type Registrar struct {
	log        *slog.Logger
	token      string
	rootNodeID uint32

	registered atomic.Bool
	mu         sync.Mutex
	lastConnID string
}

func NewRegistrar(token string, rootNodeID uint32, log *slog.Logger) *Registrar {
	if log == nil {
		log = slog.Default()
	}
	if rootNodeID == 0 {
		rootNodeID = 1
	}
	return &Registrar{log: log, token: token, rootNodeID: rootNodeID}
}

func (r *Registrar) OnParentConnected(ctx context.Context, srv core.IServer, conn core.IConnection) {
	if srv == nil || conn == nil {
		return
	}
	if r.token == "" {
		r.log.Warn("root token empty, skip root_server_register")
		return
	}
	if r.registered.Load() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.registered.Load() {
		return
	}
	payloadData, _ := json.Marshal(rootRegisterData{Token: r.token, NodeID: srv.NodeID()})
	msg := message{Action: actionRootServerRegister, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorCmd).
		WithSubProto(2).
		WithSourceID(srv.NodeID()).
		WithTargetID(r.rootNodeID)
	if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
		r.log.Error("send root_server_register failed", "err", err)
		return
	}
	r.registered.Store(true)
	r.lastConnID = conn.ID()
	r.log.Info("root_server_register sent", "target_node", r.rootNodeID, "conn", conn.ID())
}

func (r *Registrar) OnParentClosed(connID string) {
	if connID == "" {
		return
	}
	r.mu.Lock()
	if r.lastConnID == connID {
		r.registered.Store(false)
		r.lastConnID = ""
	}
	r.mu.Unlock()
}
