package login_server

import (
	"strconv"

	coreconfig "MyFlowHub-Core/internal/core/config"
)

// Config holds login server options.
type Config struct {
	Addr               string
	DSN                string
	NodeID             uint32
	ParentAddr         string
	ParentEnable       bool
	ParentReconnectSec int
	RootToken          string
	RootNodeID         uint32

	ProcessChannels   int
	ProcessWorkers    int
	ProcessBuffer     int
	SendChannels      int
	SendWorkers       int
	SendChannelBuffer int
	SendConnBuffer    int
}

func (c Config) toMapConfig() *coreconfig.MapConfig {
	data := map[string]string{
		"addr":                             c.Addr,
		coreconfig.KeyParentEnable:         strconv.FormatBool(c.ParentEnable),
		coreconfig.KeyParentAddr:           c.ParentAddr,
		coreconfig.KeyParentReconnectSec:   strconv.Itoa(maxInt(c.ParentReconnectSec, 1)),
		coreconfig.KeyProcChannelCount:     strconv.Itoa(maxInt(c.ProcessChannels, 2)),
		coreconfig.KeyProcWorkersPerChan:   strconv.Itoa(maxInt(c.ProcessWorkers, 2)),
		coreconfig.KeyProcChannelBuffer:    strconv.Itoa(maxInt(c.ProcessBuffer, 128)),
		coreconfig.KeySendChannelCount:     strconv.Itoa(maxInt(c.SendChannels, 1)),
		coreconfig.KeySendWorkersPerChan:   strconv.Itoa(maxInt(c.SendWorkers, 1)),
		coreconfig.KeySendChannelBuffer:    strconv.Itoa(maxInt(c.SendChannelBuffer, 64)),
		coreconfig.KeySendConnBuffer:       strconv.Itoa(maxInt(c.SendConnBuffer, 64)),
		coreconfig.KeySendEnqueueTimeoutMS: "200",
		"authority.node_id":                strconv.Itoa(int(c.RootNodeID)),
	}
	return coreconfig.NewMap(data)
}

func maxInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
