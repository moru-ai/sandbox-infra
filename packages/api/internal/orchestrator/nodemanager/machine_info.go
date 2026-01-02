package nodemanager

import (
	infogrpc "github.com/moru-ai/sandbox-infra/packages/shared/pkg/grpc/orchestrator-info"
	"github.com/moru-ai/sandbox-infra/packages/shared/pkg/machineinfo"
)

func (n *Node) setMachineInfo(info *infogrpc.MachineInfo) {
	n.mutex.Lock()
	defer n.mutex.Unlock()

	n.machineInfo = machineinfo.FromGRPCInfo(info)
}

func (n *Node) MachineInfo() machineinfo.MachineInfo {
	n.mutex.RLock()
	defer n.mutex.RUnlock()

	return n.machineInfo
}
