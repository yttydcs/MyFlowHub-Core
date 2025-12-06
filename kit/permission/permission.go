package permission

import (
	"strconv"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
)

const (
	Wildcard      = "*"
	AuthRevoke    = "auth.revoke"
	VarPrivateSet = "var.private_set"
)

type Config struct {
	defaultRole  string
	defaultPerms []string
	nodeRoles    map[uint32]string
	rolePerms    map[string][]string
}

func NewConfig(cfg core.IConfig) Config {
	c := Config{
		defaultRole:  "node",
		defaultPerms: []string{Wildcard},
		nodeRoles:    make(map[uint32]string),
		rolePerms:    make(map[string][]string),
	}
	c.Load(cfg)
	return c
}

func (c *Config) Load(cfg core.IConfig) {
	if cfg == nil {
		return
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthDefaultRole); ok && strings.TrimSpace(raw) != "" {
		c.defaultRole = strings.TrimSpace(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthDefaultPerms); ok {
		perms := parseList(raw)
		if len(perms) > 0 {
			c.defaultPerms = perms
		} else {
			c.defaultPerms = nil
		}
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthNodeRoles); ok {
		c.nodeRoles = parseNodeRoles(raw)
	}
	if raw, ok := cfg.Get(coreconfig.KeyAuthRolePerms); ok {
		c.rolePerms = parseRolePerms(raw)
	}
	if c.nodeRoles == nil {
		c.nodeRoles = make(map[uint32]string)
	}
	if c.rolePerms == nil {
		c.rolePerms = make(map[string][]string)
	}
}

func (c Config) ResolveRole(nodeID uint32) string {
	if nodeID != 0 {
		if role, ok := c.nodeRoles[nodeID]; ok && strings.TrimSpace(role) != "" {
			return strings.TrimSpace(role)
		}
	}
	return c.defaultRole
}

func (c Config) ResolvePerms(nodeID uint32) []string {
	role := c.ResolveRole(nodeID)
	if perms, ok := c.rolePerms[role]; ok {
		return cloneStrings(perms)
	}
	return cloneStrings(c.defaultPerms)
}

func (c Config) Has(nodeID uint32, perm string) bool {
	if perm == "" || nodeID == 0 {
		return true
	}
	perms := c.ResolvePerms(nodeID)
	if len(perms) == 0 {
		return false
	}
	for _, entry := range perms {
		if entry == Wildcard || entry == perm {
			return true
		}
	}
	return false
}

func (c Config) NodeRoles() map[uint32]string {
	if len(c.nodeRoles) == 0 {
		return nil
	}
	out := make(map[uint32]string, len(c.nodeRoles))
	for k, v := range c.nodeRoles {
		out[k] = v
	}
	return out
}

func SourceNodeID(hdr core.IHeader, conn core.IConnection) uint32 {
	if hdr != nil && hdr.SourceID() != 0 {
		return hdr.SourceID()
	}
	if conn == nil {
		return 0
	}
	if v, ok := conn.GetMeta("nodeID"); ok {
		switch val := v.(type) {
		case uint32:
			return val
		case uint64:
			return uint32(val)
		case int:
			if val >= 0 {
				return uint32(val)
			}
		case int64:
			if val >= 0 {
				return uint32(val)
			}
		}
	}
	return 0
}

func parseList(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseNodeRoles(raw string) map[uint32]string {
	m := make(map[uint32]string)
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(kv[0]), 10, 32)
		role := strings.TrimSpace(kv[1])
		if err == nil && role != "" {
			m[uint32(id)] = role
		}
	}
	return m
}

func parseRolePerms(raw string) map[string][]string {
	m := make(map[string][]string)
	pairs := strings.Split(raw, ";")
	for _, p := range pairs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			continue
		}
		role := strings.TrimSpace(kv[0])
		if role == "" {
			continue
		}
		m[role] = parseList(kv[1])
	}
	return m
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}
	out := make([]string, len(src))
	copy(out, src)
	return out
}
