package config

import "sync"

// MapConfig 是最简单的 IConfig 实现：在内存 map 中存储键值。
type MapConfig struct {
	mu   sync.RWMutex
	data map[string]string
}

// NewMap 使用传入 map 构建 MapConfig；若 data 为空则初始化为空 map。
func NewMap(data map[string]string) *MapConfig {
	mc := &MapConfig{data: make(map[string]string)}
	for k, v := range data {
		mc.data[k] = v
	}
	return mc
}

// Get 实现 core.IConfig；返回值与是否存在。
func (m *MapConfig) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[key]
	return val, ok
}

// Set 允许在运行期更新配置。
func (m *MapConfig) Set(key, val string) {
	m.mu.Lock()
	m.data[key] = val
	m.mu.Unlock()
}
