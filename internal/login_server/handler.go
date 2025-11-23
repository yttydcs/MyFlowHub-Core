package login_server

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/header"
)

const (
	actionRegister            = "register"
	actionAssistRegister      = "assist_register"
	actionRegisterResp        = "register_resp"
	actionAssistRegisterResp  = "assist_register_resp"
	actionLogin               = "login"
	actionAssistLogin         = "assist_login"
	actionLoginResp           = "login_resp"
	actionAssistLoginResp     = "assist_login_resp"
	actionRevoke              = "revoke"
	actionRevokeResp          = "revoke_resp"
	actionAssistQueryCred     = "assist_query_credential"
	actionAssistQueryCredResp = "assist_query_credential_resp"
	actionOffline             = "offline"
	actionAssistOffline       = "assist_offline"
)

type message struct {
	Action string          `json:"action"`
	Data   json.RawMessage `json:"data"`
}

type registerData struct {
	DeviceID string `json:"device_id"`
}

type loginData struct {
	DeviceID   string `json:"device_id"`
	Credential string `json:"credential"`
}

type revokeData struct {
	DeviceID   string `json:"device_id"`
	NodeID     uint32 `json:"node_id,omitempty"`
	Credential string `json:"credential,omitempty"`
}

type queryCredData struct {
	DeviceID string `json:"device_id"`
}

type respData struct {
	Code       int    `json:"code"`
	Msg        string `json:"msg,omitempty"`
	DeviceID   string `json:"device_id,omitempty"`
	NodeID     uint32 `json:"node_id,omitempty"`
	Credential string `json:"credential,omitempty"`
}

type bindingRecord struct {
	NodeID     uint32
	Credential string
}

// AuthorityHandler implements SubProto=2 as the authoritative login server backed by persistent storage.
type AuthorityHandler struct {
	log   *slog.Logger
	store Store

	cacheMu sync.RWMutex
	cache   map[string]bindingRecord
}

func NewAuthorityHandler(store Store, log *slog.Logger) *AuthorityHandler {
	if log == nil {
		log = slog.Default()
	}
	return &AuthorityHandler{
		log:   log,
		store: store,
		cache: make(map[string]bindingRecord),
	}
}

func (h *AuthorityHandler) SubProto() uint8 { return 2 }

func (h *AuthorityHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("invalid login payload", "err", err)
		return
	}
	act := strings.ToLower(strings.TrimSpace(msg.Action))
	switch act {
	case actionRegister:
		h.handleRegister(ctx, conn, hdr, msg.Data, false)
	case actionAssistRegister:
		h.handleRegister(ctx, conn, hdr, msg.Data, true)
	case actionLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, false)
	case actionAssistLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, true)
	case actionAssistQueryCred:
		h.handleAssistQuery(ctx, conn, hdr, msg.Data)
	case actionRevoke:
		h.handleRevoke(ctx, conn, hdr, msg.Data)
	case actionOffline, actionAssistOffline:
		h.handleOffline(msg.Data)
	default:
		h.log.Debug("unknown login action", "action", act)
	}
}

func (h *AuthorityHandler) handleRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req registerData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistRegisterResp, actionRegisterResp), respData{Code: 400, Msg: "invalid register data"})
		return
	}
	nodeID, cred, err := h.store.UpsertDevice(ctx, req.DeviceID)
	if err != nil {
		h.log.Error("register failed", "err", err, "device", req.DeviceID)
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistRegisterResp, actionRegisterResp), respData{Code: 500, Msg: "internal error"})
		return
	}
	h.remember(req.DeviceID, nodeID, cred)
	h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistRegisterResp, actionRegisterResp), respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     nodeID,
		Credential: cred,
	})
}

func (h *AuthorityHandler) handleLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req loginData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 400, Msg: "invalid login data"})
		return
	}
	rec, ok := h.lookup(req.DeviceID)
	if !ok {
		nodeID, cred, found, err := h.store.GetDevice(ctx, req.DeviceID)
		if err != nil {
			h.log.Error("login lookup failed", "err", err, "device", req.DeviceID)
			h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 500, Msg: "internal error"})
			return
		}
		if !found {
			h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 4001, Msg: "invalid credential"})
			return
		}
		rec = bindingRecord{NodeID: nodeID, Credential: cred}
		h.remember(req.DeviceID, rec.NodeID, rec.Credential)
	}
	if rec.Credential != req.Credential {
		h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{Code: 4001, Msg: "invalid credential"})
		return
	}
	h.sendResp(ctx, conn, hdr, chooseAction(assisted, actionAssistLoginResp, actionLoginResp), respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     rec.NodeID,
		Credential: rec.Credential,
	})
}

func (h *AuthorityHandler) handleAssistQuery(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req queryCredData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 400, Msg: "invalid query"})
		return
	}
	rec, ok := h.lookup(req.DeviceID)
	if !ok {
		nodeID, cred, found, err := h.store.GetDevice(ctx, req.DeviceID)
		if err != nil {
			h.log.Error("assist query failed", "err", err, "device", req.DeviceID)
			h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 500, Msg: "internal error"})
			return
		}
		if !found {
			h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 4001, Msg: "not found"})
			return
		}
		rec = bindingRecord{NodeID: nodeID, Credential: cred}
		h.remember(req.DeviceID, rec.NodeID, rec.Credential)
	}
	h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     rec.NodeID,
		Credential: rec.Credential,
	})
}

func (h *AuthorityHandler) handleRevoke(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	nodeID, removed, mismatch, err := h.store.DeleteDevice(ctx, req.DeviceID, req.Credential)
	if err != nil {
		h.log.Error("revoke failed", "err", err, "device", req.DeviceID)
		return
	}
	h.forget(req.DeviceID)
	if mismatch {
		h.sendResp(ctx, conn, hdr, actionRevokeResp, respData{Code: 4402, Msg: "credential mismatch", DeviceID: req.DeviceID, NodeID: nodeID})
		return
	}
	if removed {
		h.sendResp(ctx, conn, hdr, actionRevokeResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: nodeID})
	}
}

func (h *AuthorityHandler) handleOffline(data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	h.forget(req.DeviceID)
}

func (h *AuthorityHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil && conn != nil {
		if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
			h.log.Warn("send resp failed", "err", err)
		}
		return
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *AuthorityHandler) buildHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
	var base core.IHeader = &header.HeaderTcp{}
	if reqHdr != nil {
		base = reqHdr.Clone()
	}
	src := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		src = srv.NodeID()
	}
	return base.WithMajor(header.MajorOKResp).WithSubProto(2).WithSourceID(src).WithTargetID(0)
}

func (h *AuthorityHandler) remember(deviceID string, nodeID uint32, credential string) {
	h.cacheMu.Lock()
	h.cache[deviceID] = bindingRecord{NodeID: nodeID, Credential: credential}
	h.cacheMu.Unlock()
}

func (h *AuthorityHandler) forget(deviceID string) {
	h.cacheMu.Lock()
	delete(h.cache, deviceID)
	h.cacheMu.Unlock()
}

func (h *AuthorityHandler) lookup(deviceID string) (bindingRecord, bool) {
	h.cacheMu.RLock()
	rec, ok := h.cache[deviceID]
	h.cacheMu.RUnlock()
	return rec, ok
}

func chooseAction(assisted bool, assistedAct, normalAct string) string {
	if assisted {
		return assistedAct
	}
	return normalAct
}
