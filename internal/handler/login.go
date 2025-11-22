package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"

	core "MyFlowHub-Core/internal/core"
	coreconfig "MyFlowHub-Core/internal/core/config"
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
	NodeID   uint32 `json:"node_id,omitempty"`
}

type offlineData struct {
	DeviceID string `json:"device_id"`
	NodeID   uint32 `json:"node_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
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

// LoginHandler implements register/login/revoke/offline flows with action+data payload.
type LoginHandler struct {
	log *slog.Logger

	nextID atomic.Uint32

	mu          sync.RWMutex
	whitelist   map[string]bindingRecord // deviceID -> record
	pendingConn map[string]string        // deviceID -> connID (in-flight assist)

	authNode uint32
}

func NewLoginHandler(log *slog.Logger) *LoginHandler {
	if log == nil {
		log = slog.Default()
	}
	h := &LoginHandler{
		log:         log,
		whitelist:   make(map[string]bindingRecord),
		pendingConn: make(map[string]string),
	}
	h.nextID.Store(2)
	return h
}

func (h *LoginHandler) SubProto() uint8 { return 2 }

func (h *LoginHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
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
	case actionRegisterResp:
		h.handleRegisterResp(ctx, msg.Data)
	case actionLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, false)
	case actionAssistLogin:
		h.handleLogin(ctx, conn, hdr, msg.Data, true)
	case actionLoginResp:
		h.handleLoginResp(ctx, msg.Data)
	case actionRevoke:
		h.handleRevoke(ctx, conn, msg.Data)
	case actionAssistQueryCred:
		h.handleAssistQuery(ctx, conn, hdr, msg.Data)
	case actionAssistQueryCredResp:
		h.handleAssistQueryResp(ctx, msg.Data)
	case actionOffline:
		h.handleOffline(ctx, conn, msg.Data, false)
	case actionAssistOffline:
		h.handleOffline(ctx, conn, msg.Data, true)
	default:
		h.log.Debug("unknown login action", "action", act)
	}
}

// register handling
func (h *LoginHandler) handleRegister(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req registerData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionRegisterResp, respData{Code: 400, Msg: "invalid register data"})
		return
	}
	if assisted {
		// being processed at authority
		nodeID := h.ensureNodeID(req.DeviceID)
		cred := h.ensureCredential(req.DeviceID)
		h.sendResp(ctx, conn, hdr, actionAssistRegisterResp, respData{
			Code:       1,
			Msg:        "ok",
			DeviceID:   req.DeviceID,
			NodeID:     nodeID,
			Credential: cred,
		})
		return
	}
	authority := h.selectAuthority(ctx)
	if authority != nil {
		h.setPending(req.DeviceID, conn.ID())
		h.forward(ctx, authority, actionAssistRegister, req)
		return
	}
	// self authority
	nodeID := h.ensureNodeID(req.DeviceID)
	cred := h.ensureCredential(req.DeviceID)
	h.saveBinding(ctx, conn, req.DeviceID, nodeID, cred)
	h.sendResp(ctx, conn, hdr, actionRegisterResp, respData{
		Code:       1,
		Msg:        "ok",
		DeviceID:   req.DeviceID,
		NodeID:     nodeID,
		Credential: cred,
	})
}

func (h *LoginHandler) handleRegisterResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	connID, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(connID); found {
		h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, resp.Credential)
		h.sendResp(ctx, c, nil, actionRegisterResp, resp)
	}
}

// login handling
func (h *LoginHandler) handleLogin(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage, assisted bool) {
	var req loginData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 400, Msg: "invalid login data"})
		return
	}
	if assisted {
		rec, ok := h.lookup(req.DeviceID)
		if !ok || rec.Credential != req.Credential {
			h.sendResp(ctx, conn, hdr, actionAssistLoginResp, respData{Code: 4001, Msg: "invalid credential"})
			return
		}
		h.sendResp(ctx, conn, hdr, actionAssistLoginResp, respData{
			Code:       1,
			Msg:        "ok",
			DeviceID:   req.DeviceID,
			NodeID:     rec.NodeID,
			Credential: rec.Credential,
		})
		return
	}
	// local check
	if rec, ok := h.lookup(req.DeviceID); ok {
		if rec.Credential == req.Credential {
			h.saveBinding(ctx, conn, req.DeviceID, rec.NodeID, rec.Credential)
			h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: rec.NodeID, Credential: rec.Credential})
			return
		}
		h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 4001, Msg: "invalid credential"})
		return
	}
	// not found locally, try authority
	authority := h.selectAuthority(ctx)
	if authority != nil {
		h.setPending(req.DeviceID, conn.ID())
		h.forward(ctx, authority, actionAssistLogin, req)
		return
	}
	h.sendResp(ctx, conn, hdr, actionLoginResp, respData{Code: 4001, Msg: "invalid credential"})
}

func (h *LoginHandler) handleLoginResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	connID, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(connID); found {
		if resp.Code == 1 {
			h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, resp.Credential)
		}
		h.sendResp(ctx, c, nil, actionLoginResp, resp)
	}
}

// revoke handling: broadcast; respond only if deleted or credential mismatch
func (h *LoginHandler) handleRevoke(ctx context.Context, conn core.IConnection, data json.RawMessage) {
	var req revokeData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	removed, mismatch := h.removeBinding(req.DeviceID, req.Credential)
	if removed || mismatch {
		// respond only when changed/mismatch
		if mismatch {
			h.sendResp(ctx, conn, nil, actionRevokeResp, respData{Code: 4402, Msg: "credential mismatch", DeviceID: req.DeviceID, NodeID: req.NodeID})
		} else {
			h.sendResp(ctx, conn, nil, actionRevokeResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: req.NodeID})
		}
	}
	// broadcast downstream and upstream except source
	h.broadcast(ctx, conn, actionRevoke, req)
}

// assist query credential
func (h *LoginHandler) handleAssistQuery(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req queryCredData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 400, Msg: "invalid query"})
		return
	}
	if rec, ok := h.lookup(req.DeviceID); ok {
		h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 1, Msg: "ok", DeviceID: req.DeviceID, NodeID: rec.NodeID, Credential: rec.Credential})
		return
	}
	h.sendResp(ctx, conn, hdr, actionAssistQueryCredResp, respData{Code: 4001, Msg: "not found"})
}

func (h *LoginHandler) handleAssistQueryResp(ctx context.Context, data json.RawMessage) {
	var resp respData
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.DeviceID == "" {
		return
	}
	connID, ok := h.popPending(resp.DeviceID)
	if !ok {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	if c, found := srv.ConnManager().Get(connID); found {
		if resp.Code == 1 {
			h.saveBinding(ctx, c, resp.DeviceID, resp.NodeID, resp.Credential)
			h.sendResp(ctx, c, nil, actionLoginResp, respData{Code: 1, Msg: "ok", DeviceID: resp.DeviceID, NodeID: resp.NodeID, Credential: resp.Credential})
			return
		}
		h.sendResp(ctx, c, nil, actionLoginResp, respData{Code: resp.Code, Msg: resp.Msg})
	}
}

// offline handling: no response required
func (h *LoginHandler) handleOffline(ctx context.Context, conn core.IConnection, data json.RawMessage, assisted bool) {
	var req offlineData
	if err := json.Unmarshal(data, &req); err != nil || req.DeviceID == "" {
		return
	}
	h.removeBinding(req.DeviceID, "")
	h.removeIndexes(ctx, req.NodeID, conn)
	if !assisted {
		// forward to parent
		if parent := h.selectAuthorityConn(ctx); parent != nil && (conn == nil || parent.ID() != conn.ID()) {
			h.forward(ctx, parent, actionAssistOffline, req)
		}
	}
}

// helpers
func (h *LoginHandler) saveBinding(ctx context.Context, conn core.IConnection, deviceID string, nodeID uint32, cred string) {
	h.mu.Lock()
	h.whitelist[deviceID] = bindingRecord{NodeID: nodeID, Credential: cred}
	h.mu.Unlock()
	conn.SetMeta("nodeID", nodeID)
	conn.SetMeta("deviceID", deviceID)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if updater, ok := cm.(interface {
				UpdateNodeIndex(uint32, core.IConnection)
				UpdateDeviceIndex(string, core.IConnection)
			}); ok {
				updater.UpdateNodeIndex(nodeID, conn)
				updater.UpdateDeviceIndex(deviceID, conn)
			}
		}
	}
}

func (h *LoginHandler) removeBinding(deviceID, cred string) (removed bool, mismatch bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	rec, ok := h.whitelist[deviceID]
	if !ok {
		return false, false
	}
	if cred != "" && rec.Credential != cred {
		return false, true
	}
	delete(h.whitelist, deviceID)
	return true, false
}

func (h *LoginHandler) removeIndexes(ctx context.Context, nodeID uint32, conn core.IConnection) {
	if srv := core.ServerFromContext(ctx); srv != nil {
		if cm := srv.ConnManager(); cm != nil {
			if updater, ok := cm.(interface {
				UpdateNodeIndex(uint32, core.IConnection)
				UpdateDeviceIndex(string, core.IConnection)
			}); ok {
				if nodeID != 0 {
					updater.UpdateNodeIndex(nodeID, nil)
				}
			}
		}
	}
}

func (h *LoginHandler) lookup(deviceID string) (bindingRecord, bool) {
	h.mu.RLock()
	rec, ok := h.whitelist[deviceID]
	h.mu.RUnlock()
	return rec, ok
}

func (h *LoginHandler) ensureNodeID(deviceID string) uint32 {
	h.mu.RLock()
	if rec, ok := h.whitelist[deviceID]; ok {
		h.mu.RUnlock()
		return rec.NodeID
	}
	h.mu.RUnlock()
	next := h.nextID.Add(1) - 1
	return next
}

func (h *LoginHandler) ensureCredential(deviceID string) string {
	h.mu.RLock()
	if rec, ok := h.whitelist[deviceID]; ok && rec.Credential != "" {
		h.mu.RUnlock()
		return rec.Credential
	}
	h.mu.RUnlock()
	token := generateCredential()
	return token
}

func generateCredential() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

func (h *LoginHandler) setPending(deviceID, connID string) {
	h.mu.Lock()
	h.pendingConn[deviceID] = connID
	h.mu.Unlock()
}

func (h *LoginHandler) popPending(deviceID string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id, ok := h.pendingConn[deviceID]
	if ok {
		delete(h.pendingConn, deviceID)
	}
	return id, ok
}

func (h *LoginHandler) sendResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data respData) {
	msg := message{Action: action}
	raw, _ := json.Marshal(data)
	msg.Data = raw
	payload, _ := json.Marshal(msg)
	hdr := h.buildHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil {
		if conn != nil {
			if err := srv.Send(ctx, conn.ID(), hdr, payload); err != nil {
				h.log.Warn("send resp failed", "err", err)
			}
			return
		}
	}
	if conn != nil {
		codec := header.HeaderTcpCodec{}
		_ = conn.SendWithHeader(hdr, payload, codec)
	}
}

func (h *LoginHandler) buildHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
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

func (h *LoginHandler) forward(ctx context.Context, targetConn core.IConnection, action string, data any) {
	if targetConn == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv := core.ServerFromContext(ctx); srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	if nid, ok := targetConn.GetMeta("nodeID"); ok {
		if v, ok2 := nid.(uint32); ok2 {
			hdr.WithTargetID(v)
		}
	}
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, targetConn.ID(), hdr, payload)
		return
	}
	codec := header.HeaderTcpCodec{}
	_ = targetConn.SendWithHeader(hdr, payload, codec)
}

func (h *LoginHandler) selectAuthority(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if h.authNode == 0 && srv.Config() != nil {
		if raw, ok := srv.Config().Get(coreconfig.KeyParentAddr); ok && raw != "" {
			// no explicit node id, use parent conn if exists
		}
		if raw, ok := srv.Config().Get("authority.node_id"); ok {
			// optional config
			if id, err := parseUint32(raw); err == nil && id != 0 {
				h.authNode = id
			}
		}
	}
	if h.authNode != 0 {
		if c, ok := srv.ConnManager().GetByNode(h.authNode); ok {
			return c
		}
	}
	if parent := h.selectAuthorityConn(ctx); parent != nil {
		return parent
	}
	return nil
}

func (h *LoginHandler) selectAuthorityConn(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	if c, ok := findParentConnLogin(srv.ConnManager()); ok {
		return c
	}
	return nil
}

func findParentConnLogin(cm core.IConnectionManager) (core.IConnection, bool) {
	if cm == nil {
		return nil, false
	}
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if role, ok := c.GetMeta(core.MetaRoleKey); ok {
			if s, ok2 := role.(string); ok2 && s == core.RoleParent {
				parent = c
				return false
			}
		}
		return true
	})
	return parent, parent != nil
}

func (h *LoginHandler) broadcast(ctx context.Context, src core.IConnection, action string, data any) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	payloadData, _ := json.Marshal(data)
	msg := message{Action: action, Data: payloadData}
	payload, _ := json.Marshal(msg)
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(2)
	if srv != nil {
		hdr.WithSourceID(srv.NodeID())
	}
	srv.ConnManager().Range(func(c core.IConnection) bool {
		if src != nil && c.ID() == src.ID() {
			return true
		}
		if err := srv.Send(ctx, c.ID(), hdr, payload); err != nil {
			h.log.Warn("broadcast revoke failed", "conn", c.ID(), "err", err)
		}
		return true
	})
}

// Errors placeholder
var (
	ErrInvalidAction = errors.New("invalid action")
)
