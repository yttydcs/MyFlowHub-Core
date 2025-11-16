package connmgr

import (
	"errors"
	"sync"

	core "MyFlowHub-Core/internal/core"
)

// Manager 是内存连接管理器实现。
type Manager struct {
	mu    sync.RWMutex
	conns map[string]core.IConnection
	hooks core.ConnectionHooks
}

func New() *Manager {
	return &Manager{conns: make(map[string]core.IConnection)}
}

// SetHooks 注册连接钩子。
func (m *Manager) SetHooks(h core.ConnectionHooks) {
	m.mu.Lock()
	m.hooks = h
	m.mu.Unlock()
}

func (m *Manager) Add(conn core.IConnection) error {
	if conn == nil {
		return errors.New("conn nil")
	}
	m.mu.Lock()
	if _, ok := m.conns[conn.ID()]; ok {
		m.mu.Unlock()
		return errors.New("conn exists")
	}
	m.conns[conn.ID()] = conn
	h := m.hooks
	m.mu.Unlock()
	if h.OnAdd != nil {
		h.OnAdd(conn)
	}
	return nil
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	conn, ok := m.conns[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("conn not found")
	}
	delete(m.conns, id)
	h := m.hooks
	m.mu.Unlock()
	if h.OnRemove != nil {
		h.OnRemove(conn)
	}
	return conn.Close()
}

func (m *Manager) Get(id string) (core.IConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.conns[id]
	return conn, ok
}

func (m *Manager) Range(fn func(core.IConnection) bool) {
	m.mu.RLock()
	conns := make([]core.IConnection, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.mu.RUnlock()
	for _, c := range conns {
		if !fn(c) {
			return
		}
	}
}

func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.conns)
}

func (m *Manager) Broadcast(data []byte) error {
	m.Range(func(c core.IConnection) bool {
		_ = c.Send(data)
		return true
	})
	return nil
}

func (m *Manager) CloseAll() error {
	m.mu.Lock()
	conns := make([]core.IConnection, 0, len(m.conns))
	for _, c := range m.conns {
		conns = append(conns, c)
	}
	m.conns = make(map[string]core.IConnection)
	h := m.hooks
	m.mu.Unlock()
	for _, c := range conns {
		if h.OnRemove != nil {
			h.OnRemove(c)
		}
		_ = c.Close()
	}
	return nil
}
