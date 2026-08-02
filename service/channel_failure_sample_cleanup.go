package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const channelFailureSampleCleanupInterval = time.Hour

func StartChannelFailureSampleCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		cleanupChannelFailureSamples()
		ticker := time.NewTicker(channelFailureSampleCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupChannelFailureSamples()
		}
	}()
}

func cleanupChannelFailureSamples() {
	if err := model.DeleteExpiredChannelFailureSamples(time.Now()); err != nil {
		common.SysError("failed to delete expired channel failure samples: " + err.Error())
	}
}
