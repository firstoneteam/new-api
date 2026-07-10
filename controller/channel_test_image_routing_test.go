package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// TestChannelTestRoutesImageModelsToImageEndpoint guards the channel-test
// auto-detection: OpenAI image-generation models (gpt-image-1, and newer
// variants such as gpt-image-2 / gpt-image-1.5 / chatgpt-image-latest) must be
// tested against /v1/images/generations, not /v1/chat/completions. Previously
// only gpt-image-1 was recognised, so a channel serving a newer image model
// tested as a chat request and always failed.
func TestChannelTestRoutesImageModelsToImageEndpoint(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Log{}))
	service.InitHttpClient()

	user := &model.User{Username: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(user).Error)

	// Self-use mode lets unpriced test models reach the upstream, so we can
	// observe the routing the gateway actually performs.
	prevSelfUse := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = true
	t.Cleanup(func() { operation_setting.SelfUseModeEnabled = prevSelfUse })

	var (
		mu           sync.Mutex
		capturedPath string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedPath = r.URL.Path
		mu.Unlock()
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"created":1,"data":[{"url":"http://example.com/a.png"}]}`))
	}))
	defer upstream.Close()

	models := []string{"gpt-image-1", "gpt-image-2", "gpt-image-1.5", "chatgpt-image-latest"}
	for i, testModel := range models {
		t.Run(testModel, func(t *testing.T) {
			mu.Lock()
			capturedPath = ""
			mu.Unlock()

			base := upstream.URL
			key := "sk-test"
			ch := &model.Channel{
				Id:      100 + i,
				Type:    constant.ChannelTypeOpenAI,
				Key:     key,
				BaseURL: &base,
				Models:  testModel,
				Status:  common.ChannelStatusEnabled,
				Group:   "default",
			}

			res := testChannel(context.Background(), ch, user.Id, testModel, "", false)
			require.Nil(t, res.newAPIError, "unexpected API error for %s: %v", testModel, res.newAPIError)
			require.NoError(t, res.localErr, "unexpected local error for %s", testModel)

			mu.Lock()
			got := capturedPath
			mu.Unlock()
			require.Equal(t, "/v1/images/generations", got, "model %s routed to wrong upstream path", testModel)
		})
	}
}
