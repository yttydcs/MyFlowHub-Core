package tests

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	core "MyFlowHub-Core/internal/core"
	"MyFlowHub-Core/internal/core/config"
	"MyFlowHub-Core/internal/core/connmgr"
	"MyFlowHub-Core/internal/core/header"
	"MyFlowHub-Core/internal/handler"
)

type sentFrame struct {
	hdr     core.IHeader
	payload []byte
}

type recordConn struct {
	id   string
	meta map[string]any
	sent []sentFrame
	addr mockAddr
}

func newRecordConn(id string) *recordConn {
	return &recordConn{id: id, meta: make(map[string]any), addr: mockAddr{}}
}

func (c *recordConn) ID() string                           { return c.id }
func (c *recordConn) Close() error                         { return nil }
func (c *recordConn) OnReceive(core.ReceiveHandler)        {}
func (c *recordConn) SetMeta(key string, val any)          { c.meta[key] = val }
func (c *recordConn) GetMeta(key string) (any, bool)       { v, ok := c.meta[key]; return v, ok }
func (c *recordConn) Metadata() map[string]any             { return c.meta }
func (c *recordConn) LocalAddr() net.Addr                  { return c.addr }
func (c *recordConn) RemoteAddr() net.Addr                 { return c.addr }
func (c *recordConn) Reader() core.IReader                 { return nil }
func (c *recordConn) SetReader(core.IReader)               {}
func (c *recordConn) DispatchReceive(core.IHeader, []byte) {}
func (c *recordConn) RawConn() net.Conn                    { return nil }
func (c *recordConn) Send(data []byte) error {
	c.sent = append(c.sent, sentFrame{payload: data})
	return nil
}
func (c *recordConn) SendWithHeader(h core.IHeader, payload []byte, _ core.IHeaderCodec) error {
	c.sent = append(c.sent, sentFrame{hdr: h, payload: payload})
	return nil
}

type recordServer struct {
	nodeID uint32
	cm     core.IConnectionManager
	cfg    core.IConfig
	sent   []sentFrame
}

func newRecordServer(nodeID uint32, cm core.IConnectionManager) *recordServer {
	return &recordServer{
		nodeID: nodeID,
		cm:     cm,
		cfg:    config.NewMap(nil),
	}
}

func (s *recordServer) Start(context.Context) error          { return nil }
func (s *recordServer) Stop(context.Context) error           { return nil }
func (s *recordServer) Config() core.IConfig                 { return s.cfg }
func (s *recordServer) ConnManager() core.IConnectionManager { return s.cm }
func (s *recordServer) Process() core.IProcess               { return nil }
func (s *recordServer) HeaderCodec() core.IHeaderCodec       { return header.HeaderTcpCodec{} }
func (s *recordServer) NodeID() uint32                       { return s.nodeID }
func (s *recordServer) UpdateNodeID(id uint32)               { s.nodeID = id }
func (s *recordServer) Send(_ context.Context, connID string, hdr core.IHeader, payload []byte) error {
	s.sent = append(s.sent, sentFrame{hdr: hdr, payload: payload})
	if c, ok := s.cm.Get(connID); ok {
		return c.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
	}
	return nil
}

func TestVarStoreSetNewByOwner(t *testing.T) {
	h := handler.NewVarStoreHandler(nil)
	cm := connmgr.New()
	conn := newRecordConn("c1")
	conn.SetMeta("nodeID", uint32(1))
	_ = cm.Add(conn)
	srv := newRecordServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	req := setJSON("set", "temp", "v1", "public", "string")
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(1).WithTargetID(0)
	h.OnReceive(ctx, conn, hdr, req)

	if len(conn.sent) == 0 {
		t.Fatalf("no response sent")
	}
	var resp struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(conn.sent[0].payload, &resp)
	if resp.Action != "set_resp" {
		t.Fatalf("unexpected action %s", resp.Action)
	}
	var data struct {
		Code  int    `json:"code"`
		Name  string `json:"name"`
		Owner uint32 `json:"owner"`
	}
	_ = json.Unmarshal(resp.Data, &data)
	if data.Code != 1 || data.Owner != 1 || data.Name != "temp" {
		t.Fatalf("unexpected resp %+v", data)
	}
}

func TestVarStoreUpdateByOtherPublicNotify(t *testing.T) {
	h := handler.NewVarStoreHandler(nil)
	cm := connmgr.New()
	ownerConn := newRecordConn("owner")
	ownerConn.SetMeta("nodeID", uint32(10))
	_ = cm.Add(ownerConn)
	parent := newRecordConn("parent")
	parent.SetMeta("nodeID", uint32(99))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	_ = cm.Add(parent)
	srv := newRecordServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	// owner set first
	req1 := setJSON("set", "temp", "v1", "public", "")
	hdr1 := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(10).WithTargetID(0)
	h.OnReceive(ctx, ownerConn, hdr1, req1)

	// other update
	otherConn := newRecordConn("other")
	otherConn.SetMeta("nodeID", uint32(20))
	_ = cm.Add(otherConn)
	req2 := setJSON("set", "temp", "v2", "public", "")
	hdr2 := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(20).WithTargetID(0)
	h.OnReceive(ctx, otherConn, hdr2, req2)

	// owner should receive notify_update
	notified := false
	for _, f := range ownerConn.sent {
		var msg struct {
			Action string          `json:"action"`
			Data   json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(f.payload, &msg)
		if msg.Action == "notify_update" {
			notified = true
			break
		}
	}
	if !notified {
		t.Fatalf("owner not notified")
	}
}

func TestVarStorePrivateUpdateForbidden(t *testing.T) {
	h := handler.NewVarStoreHandler(nil)
	cm := connmgr.New()
	ownerConn := newRecordConn("owner")
	ownerConn.SetMeta("nodeID", uint32(10))
	_ = cm.Add(ownerConn)
	srv := newRecordServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	req1 := setJSON("set", "secret", "v1", "private", "")
	hdr1 := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(10).WithTargetID(0)
	h.OnReceive(ctx, ownerConn, hdr1, req1)

	other := newRecordConn("other")
	other.SetMeta("nodeID", uint32(20))
	_ = cm.Add(other)
	req2 := setJSON("set", "secret", "v2", "private", "")
	hdr2 := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(20).WithTargetID(0)
	h.OnReceive(ctx, other, hdr2, req2)

	var resp struct {
		Action string          `json:"action"`
		Data   json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(other.sent[0].payload, &resp)
	var data struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal(resp.Data, &data)
	if data.Code != 403 {
		t.Fatalf("expected 403, got %+v", data)
	}
}

func TestVarStoreGetMissForwardAndCache(t *testing.T) {
	h := handler.NewVarStoreHandler(nil)
	cm := connmgr.New()
	child := newRecordConn("child")
	child.SetMeta("nodeID", uint32(2))
	_ = cm.Add(child)
	parent := newRecordConn("parent")
	parent.SetMeta("nodeID", uint32(99))
	parent.SetMeta(core.MetaRoleKey, core.RoleParent)
	_ = cm.Add(parent)
	srv := newRecordServer(1, cm)
	ctx := core.WithServerContext(context.Background(), srv)

	req := getJSON("get", "temp")
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(2).WithTargetID(0)
	h.OnReceive(ctx, child, hdr, req)

	if len(parent.sent) == 0 {
		t.Fatalf("expected forward to parent")
	}
	// simulate assist_get_resp from upstream
	resp := map[string]any{"code": 1, "name": "temp", "value": "v1", "owner": 5, "visibility": "public", "type": "string"}
	respMsg := mustJSON(map[string]any{"action": "assist_get_resp", "data": resp})
	respHdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(3).WithSourceID(99).WithTargetID(2)
	h.OnReceive(ctx, child, respHdr, respMsg)

	// child should have received get_resp
	found := false
	for _, f := range child.sent {
		var msg struct {
			Action string          `json:"action"`
			Data   json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(f.payload, &msg)
		if msg.Action == "get_resp" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected get_resp to child")
	}
}

func setJSON(action, name, value, vis, typ string) []byte {
	data := map[string]any{
		"name":       name,
		"value":      value,
		"visibility": vis,
	}
	if typ != "" {
		data["type"] = typ
	}
	raw, _ := json.Marshal(map[string]any{
		"action": action,
		"data":   data,
	})
	return raw
}

func getJSON(action, name string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"action": action,
		"data":   map[string]any{"name": name},
	})
	return raw
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
