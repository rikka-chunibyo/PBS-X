package sync

import (
	"github.com/fxamacker/cbor/v2"
	"github.com/pbs-x/pbsx-host-agent/internal/agent/agentfs/fswire"
	"github.com/pbs-x/pbsx-host-agent/internal/arpc"
	"github.com/pbs-x/pbsx-host-agent/internal/filetree"
)

func FileTreeHandler(req *arpc.Request, rpcSess *arpc.StreamPipe) (arpc.Response, error) {
	var reqData fswire.FileTreeReq
	err := cbor.Unmarshal(req.Payload, &reqData)
	if err != nil {
		return arpc.Response{}, err
	}

	resp, err := filetree.Read(reqData.HostPath, reqData.SubPath)
	if err != nil {
		return arpc.Response{}, err
	}

	encoded, err := cbor.Marshal(resp)
	if err != nil {
		return arpc.Response{}, err
	}

	return arpc.Response{Status: 200, Data: encoded}, nil
}
