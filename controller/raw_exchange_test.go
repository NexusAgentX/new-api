package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRawExchangeBodyAndDeleteEndpointsRequireScopedSecurityProof(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousSecret := common.SessionSecret
	common.SessionSecret = "raw-exchange-proof-test-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })

	identity := service.AuthIdentity{
		UserID:          7,
		SessionID:       "raw-exchange-proof-session",
		UserAuthVersion: 1,
		SessionVersion:  1,
	}
	wrongScopeProof, _, err := service.IssueSecurityProof(identity, secureVerificationMethod2FA, []string{securityProofScopeRawExchangeRead})
	require.NoError(t, err)

	tests := []struct {
		name         string
		method       string
		path         string
		handler      gin.HandlerFunc
		proof        string
		expectedCode string
	}{
		{name: "request download", method: http.MethodGet, path: "/api/raw-exchanges/self/request-id/request", handler: DownloadRawExchangeRequest, expectedCode: "SECURITY_PROOF_REQUIRED"},
		{name: "response download", method: http.MethodGet, path: "/api/raw-exchanges/self/request-id/response", handler: DownloadRawExchangeResponse, expectedCode: "SECURITY_PROOF_REQUIRED"},
		{name: "bundle download", method: http.MethodGet, path: "/api/raw-exchanges/self/request-id/bundle", handler: DownloadRawExchangeBundle, expectedCode: "SECURITY_PROOF_REQUIRED"},
		{name: "single delete rejects read proof", method: http.MethodDelete, path: "/api/raw-exchanges/self/request-id", handler: DeleteRawExchange, proof: wrongScopeProof, expectedCode: "SECURITY_PROOF_SCOPE_MISMATCH"},
		{name: "batch delete", method: http.MethodPost, path: "/api/raw-exchanges/self/batch-delete", handler: DeleteRawExchangeBatch, expectedCode: "SECURITY_PROOF_REQUIRED"},
		{name: "admin cleanup", method: http.MethodPost, path: "/api/system-task/raw-exchange-cleanup", handler: CreateRawExchangeCleanupSystemTask, expectedCode: "SECURITY_PROOF_REQUIRED"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			if test.proof != "" {
				request.Header.Set("X-Security-Proof", test.proof)
			}
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = request
			context.Params = gin.Params{{Key: "request_id", Value: "request-id"}}
			context.Set("id", identity.UserID)
			context.Set("session_id", identity.SessionID)
			context.Set("auth_version", identity.UserAuthVersion)
			context.Set("session_version", identity.SessionVersion)

			test.handler(context)

			assert.Equal(t, http.StatusForbidden, response.Code)
			var responseBody struct {
				Code string `json:"code"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &responseBody))
			assert.Equal(t, test.expectedCode, responseBody.Code)
		})
	}
}

func TestRawExchangeCleanupDetectsSemanticFullCleanup(t *testing.T) {
	assert.False(t, rawExchangeCleanupMatchesAll(model.RawExchangeCleanupFilter{AfterTimestamp: 100}))
	assert.False(t, rawExchangeCleanupMatchesAll(model.RawExchangeCleanupFilter{BeforeTimestamp: 200}))
	assert.False(t, rawExchangeCleanupMatchesAll(model.RawExchangeCleanupFilter{Statuses: []string{model.RawExchangeStatusStored}}))
	assert.True(t, rawExchangeCleanupMatchesAll(model.RawExchangeCleanupFilter{}))
	assert.True(t, rawExchangeCleanupMatchesAll(model.RawExchangeCleanupFilter{Statuses: []string{
		model.RawExchangeStatusPendingCommit,
		model.RawExchangeStatusStored,
		model.RawExchangeStatusSkippedTooLarge,
		model.RawExchangeStatusSkippedCapacity,
		model.RawExchangeStatusCaptureFailed,
		model.RawExchangeStatusDeletePending,
		model.RawExchangeStatusDeleteFailed,
		model.RawExchangeStatusDeleted,
		model.RawExchangeStatusMissing,
	}}))
}

func TestRawExchangeSecurityProofScopesAreAllowed(t *testing.T) {
	assert.True(t, isAllowedSecurityProofScope(securityProofScopeRawExchangeRead))
	assert.True(t, isAllowedSecurityProofScope(securityProofScopeRawExchangeDelete))
	assert.True(t, isAllowedSecurityProofScope(securityProofScopeRawExchangeAdminCleanup))
}
