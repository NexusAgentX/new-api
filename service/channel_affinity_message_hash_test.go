package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newChannelAffinityMessageHashContext(t *testing.T, path, body string) *gin.Context {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	return ctx
}

func withChannelAffinityRule(t *testing.T, rule operation_setting.ChannelAffinityRule) {
	t.Helper()
	setting := operation_setting.GetChannelAffinitySetting()
	original := *setting
	setting.Enabled = true
	setting.Rules = append([]operation_setting.ChannelAffinityRule{rule}, setting.Rules...)
	t.Cleanup(func() {
		*setting = original
	})
}

func messageHashRule(name string) operation_setting.ChannelAffinityRule {
	return operation_setting.ChannelAffinityRule{
		Name:              name,
		ModelRegex:        []string{"^gpt-5$"},
		PathRegex:         []string{"/v1/chat/completions"},
		KeySources:        []operation_setting.ChannelAffinityKeySource{{Type: channelAffinityMessageHashSource}},
		IncludeUsingGroup: true,
		IncludeModelName:  true,
		IncludeRuleName:   true,
	}
}

func TestExtractChannelAffinityMessageHashesNormalizesJSONFormatting(t *testing.T) {
	first := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{
  "messages": [
    {"role":"system","content":"Be concise"},
    {"role":"user","content":[{"type":"text","text":"Hello"}]}
  ]
}`)
	second := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"content":"Be concise","role":"system"},{"content":[{"text":"Hello","type":"text"}],"role":"user"}]}`)

	firstHashes, firstOK := extractChannelAffinityMessageHashes(first)
	secondHashes, secondOK := extractChannelAffinityMessageHashes(second)
	require.True(t, firstOK)
	require.True(t, secondOK)
	require.Equal(t, "openai_chat", firstHashes.Protocol)
	require.Equal(t, firstHashes.Values, secondHashes.Values)
	require.Len(t, firstHashes.Values, 1)
	require.Len(t, firstHashes.Values[0], 64)
}

func TestExtractChannelAffinityMessageHashesSupportsConversationProtocols(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		body         string
		wantProtocol string
	}{
		{
			name:         "OpenAI Chat Completions",
			path:         "/v1/chat/completions",
			body:         `{"messages":[{"role":"developer","content":"Be concise"},{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi"}]}`,
			wantProtocol: "openai_chat",
		},
		{
			name:         "OpenAI Responses",
			path:         "/v1/responses",
			body:         `{"instructions":"Be concise","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Hello"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hi"}]}]}`,
			wantProtocol: "openai_responses",
		},
		{
			name:         "Anthropic Messages",
			path:         "/v1/messages",
			body:         `{"system":[{"type":"text","text":"Be concise"}],"messages":[{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi"}]}`,
			wantProtocol: "anthropic_messages",
		},
		{
			name:         "Gemini native",
			path:         "/v1beta/models/gemini-2.5-flash:generateContent",
			body:         `{"system_instruction":{"parts":[{"text":"Be concise"}]},"contents":[{"role":"user","parts":[{"text":"Hello"}]},{"role":"model","parts":[{"text":"Hi"}]}]}`,
			wantProtocol: "gemini",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := newChannelAffinityMessageHashContext(t, test.path, test.body)
			hashes, ok := extractChannelAffinityMessageHashes(ctx)
			require.True(t, ok)
			require.Equal(t, test.wantProtocol, hashes.Protocol)
			require.Len(t, hashes.Values, 2)
			require.NotEqual(t, hashes.Values[0], hashes.Values[1])
		})
	}
}

func TestGetPreferredChannelByAffinityMessageHashHonorsExplicitSource(t *testing.T) {
	rule := messageHashRule("message-hash-explicit-priority")
	rule.KeySources = []operation_setting.ChannelAffinityKeySource{
		{Type: channelAffinityMessageHashSource},
		{Type: "gjson", Path: "prompt_cache_key"},
	}
	withChannelAffinityRule(t, rule)

	ctx := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"model":"gpt-5","prompt_cache_key":"explicit-session","messages":[{"role":"user","content":"same prompt"}]}`)
	explicitSuffix := buildChannelAffinityCacheKeySuffix(rule, "gpt-5", "default", "explicit-session")
	messageHashes, ok := extractChannelAffinityMessageHashes(ctx)
	require.True(t, ok)
	hashKeys, _ := buildChannelAffinityCacheKeys(rule, "gpt-5", "default", channelAffinityMessageHashSource, messageHashes.Protocol, messageHashes.Values)
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(explicitSuffix, 1201, time.Minute))
	require.NoError(t, cache.SetWithTTL(hashKeys[0], 1202, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany(append([]string{explicitSuffix}, hashKeys...))
	})

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 1201, channelID)
	meta, ok := getChannelAffinityMeta(ctx)
	require.True(t, ok)
	require.Equal(t, "gjson", meta.KeySourceType)
}

func TestChannelAffinityMessageHashInheritsInitialAnchorAcrossTurns(t *testing.T) {
	rule := messageHashRule("message-hash-multi-turn")
	withChannelAffinityRule(t, rule)

	firstBody := `{"model":"gpt-5","messages":[{"role":"system","content":"Be concise"},{"role":"user","content":"Hello"}]}`
	firstCtx := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", firstBody)
	firstHashes, ok := extractChannelAffinityMessageHashes(firstCtx)
	require.True(t, ok)
	require.Len(t, firstHashes.Values, 1)
	firstKeys, firstSuffixes := buildChannelAffinityCacheKeys(rule, "gpt-5", "default", channelAffinityMessageHashSource, firstHashes.Protocol, firstHashes.Values)
	require.Len(t, firstKeys, 1)

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(firstSuffixes[0], 1203, time.Minute))

	secondBody := `{"model":"gpt-5","messages":[{"role":"system","content":"Be concise"},{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi"},{"role":"user","content":"Continue"}]}`
	secondCtx := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", secondBody)
	channelID, found := GetPreferredChannelByAffinity(secondCtx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 1203, channelID)

	secondHashes, ok := extractChannelAffinityMessageHashes(secondCtx)
	require.True(t, ok)
	require.Len(t, secondHashes.Values, 2)
	secondKeys, secondSuffixes := buildChannelAffinityCacheKeys(rule, "gpt-5", "default", channelAffinityMessageHashSource, secondHashes.Protocol, secondHashes.Values)
	require.Len(t, secondKeys, 2)
	require.NotEqual(t, firstKeys[0], secondKeys[0])

	RecordChannelAffinity(secondCtx, channelID)
	stored, found, err := cache.Get(secondSuffixes[0])
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, channelID, stored)
	t.Cleanup(func() {
		_, _ = cache.DeleteMany(append(firstKeys, secondKeys...))
	})
}

func TestChannelAffinityMessageHashSeparatesDifferentContent(t *testing.T) {
	first := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"user","content":"first prompt"}]}`)
	second := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"user","content":"second prompt"}]}`)

	firstHashes, firstOK := extractChannelAffinityMessageHashes(first)
	secondHashes, secondOK := extractChannelAffinityMessageHashes(second)
	require.True(t, firstOK)
	require.True(t, secondOK)
	require.NotEqual(t, firstHashes.Values[0], secondHashes.Values[0])

	system := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"system","content":"same instruction"},{"role":"user","content":"prompt"}]}`)
	developer := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"developer","content":"same instruction"},{"role":"user","content":"prompt"}]}`)
	systemHashes, systemOK := extractChannelAffinityMessageHashes(system)
	developerHashes, developerOK := extractChannelAffinityMessageHashes(developer)
	require.True(t, systemOK)
	require.True(t, developerOK)
	require.NotEqual(t, systemHashes.Values[0], developerHashes.Values[0])
}

func TestChannelAffinityMessageHashSeparatesProtocolAndGroup(t *testing.T) {
	openAIRule := messageHashRule("message-hash-protocol-isolation")
	withChannelAffinityRule(t, openAIRule)

	openAIContext := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"user","content":"same prompt"}]}`)
	openAIHashes, ok := extractChannelAffinityMessageHashes(openAIContext)
	require.True(t, ok)

	geminiContext := newChannelAffinityMessageHashContext(t, "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"same prompt"}]}]}`)
	geminiHashes, ok := extractChannelAffinityMessageHashes(geminiContext)
	require.True(t, ok)
	require.Equal(t, "gemini", geminiHashes.Protocol)
	require.NotEqual(t, openAIHashes.Values[0], geminiHashes.Values[0])

	openAIKeys, openAISuffixes := buildChannelAffinityCacheKeys(openAIRule, "gpt-5", "group-a", channelAffinityMessageHashSource, openAIHashes.Protocol, openAIHashes.Values)
	groupBKeys, groupBSuffixes := buildChannelAffinityCacheKeys(openAIRule, "gpt-5", "group-b", channelAffinityMessageHashSource, openAIHashes.Protocol, openAIHashes.Values)
	require.NotEqual(t, openAIKeys[0], groupBKeys[0])

	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(openAISuffixes[0], 1204, time.Minute))
	groupBContext := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"user","content":"same prompt"}]}`)
	channelID, found := GetPreferredChannelByAffinity(groupBContext, "gpt-5", "group-b")
	require.False(t, found)
	require.Zero(t, channelID)
	t.Cleanup(func() {
		_, _ = cache.DeleteMany(append(openAIKeys, groupBKeys...))
		_, _ = cache.DeleteMany(append(openAISuffixes, groupBSuffixes...))
	})
}

func TestClearCurrentChannelAffinityCacheDeletesMessageHashAliases(t *testing.T) {
	rule := messageHashRule("message-hash-clear-aliases")
	withChannelAffinityRule(t, rule)

	ctx := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"user","content":"Hello"},{"role":"assistant","content":"Hi"}]}`)
	hashes, ok := extractChannelAffinityMessageHashes(ctx)
	require.True(t, ok)
	keys, suffixes := buildChannelAffinityCacheKeys(rule, "gpt-5", "default", channelAffinityMessageHashSource, hashes.Protocol, hashes.Values)
	require.Len(t, keys, 2)

	cache := getChannelAffinityCache()
	for _, suffix := range suffixes {
		require.NoError(t, cache.SetWithTTL(suffix, 1206, time.Minute))
	}
	t.Cleanup(func() {
		_, _ = cache.DeleteMany(keys)
	})

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 1206, channelID)
	require.True(t, ClearCurrentChannelAffinityCache(ctx))
	for _, suffix := range suffixes {
		_, found, err := cache.Get(suffix)
		require.NoError(t, err)
		require.False(t, found)
	}
}

func TestChannelAffinityMessageHashDoesNotLogPromptContent(t *testing.T) {
	rule := messageHashRule("message-hash-log-safety")
	withChannelAffinityRule(t, rule)

	prompt := "sensitive prompt that must never appear in affinity logs"
	ctx := newChannelAffinityMessageHashContext(t, "/v1/chat/completions", `{"messages":[{"role":"user","content":"`+prompt+`"}]}`)
	hashes, ok := extractChannelAffinityMessageHashes(ctx)
	require.True(t, ok)
	keys, suffixes := buildChannelAffinityCacheKeys(rule, "gpt-5", "default", channelAffinityMessageHashSource, hashes.Protocol, hashes.Values)
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(suffixes[0], 1205, time.Minute))
	t.Cleanup(func() {
		_, _ = cache.DeleteMany(keys)
	})

	channelID, found := GetPreferredChannelByAffinity(ctx, "gpt-5", "default")
	require.True(t, found)
	require.Equal(t, 1205, channelID)
	MarkChannelAffinityUsed(ctx, "default", channelID)
	info, ok := ctx.Get(ginKeyChannelAffinityLogInfo)
	require.True(t, ok)
	encoded, err := common.Marshal(info)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), prompt)
	require.NotContains(t, string(encoded), "sensitive prompt")
}
