package model

const (
	UsageRequestTypeRegular     = "regular"
	UsageRequestTypeChannelTest = "channel_test"
)

func normalizeUsageRequestType(requestType string) string {
	if requestType == "" {
		return UsageRequestTypeRegular
	}
	return requestType
}

func isChannelTestUsage(requestType string, tokenID int, tokenName string) bool {
	if requestType == UsageRequestTypeChannelTest {
		return true
	}
	return tokenID == 0 && tokenName == "模型测试"
}
