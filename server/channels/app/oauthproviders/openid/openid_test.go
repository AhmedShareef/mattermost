// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package oauthopenid

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSSOSettingsResolvesDiscoveryEndpoints(t *testing.T) {
	rctx := request.TestContext(t)
	provider := &OpenIDProvider{}

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/.well-known/openid-configuration", r.URL.Path)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": server.URL + "/authorize",
			"token_endpoint":         server.URL + "/token",
			"userinfo_endpoint":      server.URL + "/userinfo",
		}))
	}))
	defer server.Close()

	config := &model.Config{}
	config.SetDefaults()
	config.OpenIdSettings.Enable = model.NewPointer(true)
	config.OpenIdSettings.DiscoveryEndpoint = model.NewPointer(server.URL + "/.well-known/openid-configuration")
	config.OpenIdSettings.AuthEndpoint = model.NewPointer("")
	config.OpenIdSettings.TokenEndpoint = model.NewPointer("")
	config.OpenIdSettings.UserAPIEndpoint = model.NewPointer("")

	settings, err := provider.GetSSOSettings(rctx, config, model.ServiceOpenid)
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/authorize", *settings.AuthEndpoint)
	assert.Equal(t, server.URL+"/token", *settings.TokenEndpoint)
	assert.Equal(t, server.URL+"/userinfo", *settings.UserAPIEndpoint)

	// The provider resolves endpoints per request without mutating the stored config.
	assert.Empty(t, *config.OpenIdSettings.AuthEndpoint)
	assert.Empty(t, *config.OpenIdSettings.TokenEndpoint)
	assert.Empty(t, *config.OpenIdSettings.UserAPIEndpoint)
}

func TestGetSSOSettingsUsesExplicitEndpoints(t *testing.T) {
	rctx := request.TestContext(t)
	provider := &OpenIDProvider{}
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := &model.Config{}
	config.SetDefaults()
	config.OpenIdSettings.Enable = model.NewPointer(true)
	config.OpenIdSettings.DiscoveryEndpoint = model.NewPointer(server.URL + "/.well-known/openid-configuration")
	config.OpenIdSettings.AuthEndpoint = model.NewPointer(server.URL + "/authorize")
	config.OpenIdSettings.TokenEndpoint = model.NewPointer(server.URL + "/token")
	config.OpenIdSettings.UserAPIEndpoint = model.NewPointer(server.URL + "/userinfo")

	settings, err := provider.GetSSOSettings(rctx, config, model.ServiceOpenid)
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/authorize", *settings.AuthEndpoint)
	assert.Equal(t, server.URL+"/token", *settings.TokenEndpoint)
	assert.Equal(t, server.URL+"/userinfo", *settings.UserAPIEndpoint)
	assert.Zero(t, calls.Load())
}

func TestGetUserFromJSON(t *testing.T) {
	rctx := request.TestContext(t)
	provider := &OpenIDProvider{}

	userInfo := map[string]string{
		"sub":         "user-123",
		"email":       "Test.User@example.com",
		"given_name":  "Test",
		"family_name": "User",
	}

	body, err := json.Marshal(userInfo)
	require.NoError(t, err)

	user, err := provider.GetUserFromJSON(rctx, bytes.NewReader(body), nil, &model.SSOSettings{})
	require.NoError(t, err)
	assert.Equal(t, "test.user", user.Username)
	assert.Equal(t, "Test", user.FirstName)
	assert.Equal(t, "User", user.LastName)
	assert.Equal(t, "test.user@example.com", user.Email)
	require.NotNil(t, user.AuthData)
	assert.Equal(t, "user-123", *user.AuthData)
}

func TestGetUserFromJSONUsesPreferredUsername(t *testing.T) {
	rctx := request.TestContext(t)
	provider := &OpenIDProvider{}

	body := `{"sub":"user-123","email":"person@example.com","preferred_username":"preferred.user@example.com"}`
	settings := &model.SSOSettings{UsePreferredUsername: model.NewPointer(true)}

	user, err := provider.GetUserFromJSON(rctx, bytes.NewBufferString(body), nil, settings)
	require.NoError(t, err)
	assert.Equal(t, "preferred.user", user.Username)
}

func TestGetUserFromJSONMergesIDTokenClaims(t *testing.T) {
	rctx := request.TestContext(t)
	provider := &OpenIDProvider{}

	tokenUser := &model.User{
		Username:  "token.user",
		FirstName: "Token",
		LastName:  "User",
		Email:     "token.user@example.com",
		AuthData:  model.NewPointer("user-123"),
	}

	user, err := provider.GetUserFromJSON(rctx, bytes.NewBufferString(`{"sub":"user-123"}`), tokenUser, &model.SSOSettings{})
	require.NoError(t, err)
	assert.Equal(t, "token.user", user.Username)
	assert.Equal(t, "Token", user.FirstName)
	assert.Equal(t, "User", user.LastName)
	assert.Equal(t, "token.user@example.com", user.Email)
	require.NotNil(t, user.AuthData)
	assert.Equal(t, "user-123", *user.AuthData)
}

func TestGetUserFromJSONIgnoresMismatchedIDTokenClaims(t *testing.T) {
	rctx := request.TestContext(t)
	provider := &OpenIDProvider{}

	tokenUser := &model.User{
		Email:    "token.user@example.com",
		AuthData: model.NewPointer("different-user"),
	}

	_, err := provider.GetUserFromJSON(rctx, bytes.NewBufferString(`{"sub":"user-123"}`), tokenUser, &model.SSOSettings{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user e-mail should not be empty")
}

func TestGetUserFromIdToken(t *testing.T) {
	provider := &OpenIDProvider{}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user-123","email":"oidc.user@example.com","preferred_username":"oidc.user@example.com","given_name":"OIDC","family_name":"User"}`))
	idToken := header + "." + payload + "."

	user, err := provider.GetUserFromIdToken(nil, idToken)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, "oidc.user", user.Username)
	assert.Equal(t, "OIDC", user.FirstName)
	assert.Equal(t, "User", user.LastName)
	assert.Equal(t, "oidc.user@example.com", user.Email)
	require.NotNil(t, user.AuthData)
	assert.Equal(t, "user-123", *user.AuthData)
}
