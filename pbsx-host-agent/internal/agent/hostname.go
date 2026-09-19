package agent

import (
	"hash/fnv"
	"strconv"
	"time"

	"github.com/pbs-x/pbsx-host-agent/internal/conf"
	"github.com/pbs-x/pbsx-host-agent/internal/crypto"
	"github.com/pbs-x/pbsx-host-agent/internal/host"
)

func ComputeDelay() time.Duration {
	fixedDelayEnv := conf.Env.UpdateIntervalMin
	fixedDelay := -1

	if fixedDelayEnv != "" {
		var err error
		fixedDelay, err = strconv.Atoi(fixedDelayEnv)
		if err != nil {
			fixedDelay = -1
		}
	}

	if fixedDelay != -1 {
		return time.Duration(fixedDelay) * time.Minute
	}

	const baseSeconds = 3600
	const extraRangeSeconds = 3600

	hostname, err := host.AgentHostname()
	if err != nil {
		hostname = randomHostname()
	}

	hashVal := hashHostname(hostname)
	extraSeconds := int(hashVal % uint32(extraRangeSeconds))
	return time.Duration(baseSeconds+extraSeconds) * time.Second
}
func hashHostname(host string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(host))
	return h.Sum32()
}

func randomHostname() string {
	n, err := crypto.SecureRandomInt(1000)
	if err != nil {
		n = 0
	}
	return time.Now().Format("20060102150405") + strconv.Itoa(int(n))
}
