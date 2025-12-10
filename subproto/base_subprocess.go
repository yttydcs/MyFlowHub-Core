package subproto

import (
	"context"

	core "github.com/yttydcs/myflowhub-core"
)

// BaseSubProcess 提供 ISubProcess 的默认实现与可选方法实现，便于嵌入减少样板。
// 建议实际 handler 覆盖 SubProto/OnReceive/Init 等方法。
type BaseSubProcess struct{}

// 默认子协议编号 0（需实际 handler 覆盖）。
func (BaseSubProcess) SubProto() uint8 { return 0 }

// 默认 OnReceive 空实现（需实际 handler 覆盖）。
func (BaseSubProcess) OnReceive(context.Context, core.IConnection, core.IHeader, []byte) {}

// 默认 Init 直接返回 true。
func (BaseSubProcess) Init() bool { return true }

// 默认不截获 Cmd。
func (BaseSubProcess) AcceptCmd() bool { return false }

// 默认不允许 Source 与连接元数据不一致。
func (BaseSubProcess) AllowSourceMismatch() bool { return false }
