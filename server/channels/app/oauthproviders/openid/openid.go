// Copyright (c) 2015-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package oauthopenid

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/shared/mlog"
	"github.com/mattermost/mattermost/server/public/shared/request"
	"github.com/mattermost/mattermost/server/v8/einterfaces"
)

const discoveryRequestTimeout = 10 * time.Second

type OpenIDProvider struct{}

type openIDDiscoveryDocument struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
}

type openIDUserInfo struct {
	Subject           string `json:"sub"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Nickname          string `json:"nickname"`
}

func init() {
	einterfaces.RegisterOAuthProvider(model.ServiceOpenid, &OpenIDProvider{})
}

func (op *OpenIDProvider) GetUserFromJSON(rctx request.CTX, data io.Reader, tokenUser *model.User, settings *model.SSOSettings) (*model.User, error) {
	claims, err := openIDUserFromJSON(data)
	if err != nil {
		return nil, err
	}

	mergeClaimsFromTokenUser(claims, tokenUser)
	claims.normalize()

	if err := claims.IsValid(); err != nil {
		return nil, err
	}

	return userFromOpenIDClaims(rctx, claims, settings), nil
}

func (op *OpenIDProvider) GetSSOSettings(rctx request.CTX, config *model.Config, service string) (*model.SSOSettings, error) {
	settings := config.GetSSOService(service)
	if settings == nil {
		return nil, fmt.Errorf("missing OpenID Connect settings for %s", service)
	}

	resolved := cloneSSOSettings(settings)
	if needsDiscovery(resolved) {
		discovery, err := fetchDiscoveryDocument(*resolved.DiscoveryEndpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve OpenID Connect discovery document for %s: %w", service, err)
		}

		if stringValue(resolved.AuthEndpoint) == "" {
			resolved.AuthEndpoint = model.NewPointer(discovery.AuthorizationEndpoint)
		}
		if stringValue(resolved.TokenEndpoint) == "" {
			resolved.TokenEndpoint = model.NewPointer(discovery.TokenEndpoint)
		}
		if stringValue(resolved.UserAPIEndpoint) == "" {
			resolved.UserAPIEndpoint = model.NewPointer(discovery.UserInfoEndpoint)
		}
	}

	if stringValue(resolved.AuthEndpoint) == "" || stringValue(resolved.TokenEndpoint) == "" || stringValue(resolved.UserAPIEndpoint) == "" {
		return nil, fmt.Errorf("missing OpenID Connect endpoints for %s", service)
	}

	return resolved, nil
}

func (op *OpenIDProvider) GetUserFromIdToken(_ request.CTX, idToken string) (*model.User, error) {
	claims, err := openIDUserFromJWT(idToken)
	if err != nil {
		// Treat id_token claims as optional enrichment only. The userinfo response remains authoritative.
		return nil, nil
	}

	claims.normalize()
	if claims.Subject == "" && claims.Email == "" {
		return nil, nil
	}

	return userFromOpenIDClaims(nil, claims, nil), nil
}

func (op *OpenIDProvider) IsSameUser(_ request.CTX, dbUser, oauthUser *model.User) bool {
	return authDataValue(dbUser.AuthData) == authDataValue(oauthUser.AuthData)
}

func openIDUserFromJSON(data io.Reader) (*openIDUserInfo, error) {
	var claims openIDUserInfo
	if err := json.NewDecoder(data).Decode(&claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func openIDUserFromJWT(idToken string) (*openIDUserInfo, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid id_token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}

	return openIDUserFromJSON(strings.NewReader(string(payload)))
}

func (u *openIDUserInfo) normalize() {
	u.Subject = strings.TrimSpace(u.Subject)
	u.Name = strings.TrimSpace(u.Name)
	u.GivenName = strings.TrimSpace(u.GivenName)
	u.FamilyName = strings.TrimSpace(u.FamilyName)
	u.Email = strings.ToLower(strings.TrimSpace(u.Email))
	u.PreferredUsername = strings.TrimSpace(u.PreferredUsername)
	u.Nickname = strings.TrimSpace(u.Nickname)
}

func (u *openIDUserInfo) IsValid() error {
	if u.Subject == "" {
		return errors.New("user subject should not be empty")
	}
	if u.Email == "" {
		return errors.New("user e-mail should not be empty")
	}
	return nil
}

func userFromOpenIDClaims(rctx request.CTX, claims *openIDUserInfo, settings *model.SSOSettings) *model.User {
	user := &model.User{}

	username := resolveUsername(claims, settings)
	user.Username = model.CleanUsername(usernameLogger(rctx), username)

	firstName, lastName := resolveNames(claims)
	user.FirstName = firstName
	user.LastName = lastName
	user.Email = claims.Email

	authData := claims.Subject
	user.AuthData = &authData

	return user
}

func resolveUsername(claims *openIDUserInfo, settings *model.SSOSettings) string {
	switch {
	case settings != nil && model.SafeDereference(settings.UsePreferredUsername) && claims.PreferredUsername != "":
		return stripDomain(claims.PreferredUsername)
	case claims.Nickname != "":
		return claims.Nickname
	case claims.Email != "":
		return stripDomain(claims.Email)
	case claims.PreferredUsername != "":
		return stripDomain(claims.PreferredUsername)
	case claims.Name != "":
		return claims.Name
	default:
		return claims.Subject
	}
}

func resolveNames(claims *openIDUserInfo) (string, string) {
	if claims.GivenName != "" || claims.FamilyName != "" {
		return claims.GivenName, claims.FamilyName
	}

	parts := strings.Fields(claims.Name)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return parts[0], ""
	default:
		return parts[0], strings.Join(parts[1:], " ")
	}
}

func mergeClaimsFromTokenUser(claims *openIDUserInfo, tokenUser *model.User) {
	if tokenUser == nil {
		return
	}

	tokenSubject := authDataValue(tokenUser.AuthData)
	if tokenSubject == "" {
		return
	}
	if claims.Subject != "" && claims.Subject != tokenSubject {
		return
	}

	if claims.Subject == "" {
		claims.Subject = tokenSubject
	}
	if claims.Email == "" {
		claims.Email = tokenUser.Email
	}
	if claims.GivenName == "" {
		claims.GivenName = tokenUser.FirstName
	}
	if claims.FamilyName == "" {
		claims.FamilyName = tokenUser.LastName
	}
	if claims.Name == "" {
		claims.Name = tokenUser.GetFullName()
	}
	if claims.PreferredUsername == "" {
		claims.PreferredUsername = tokenUser.Username
	}
	if claims.Nickname == "" {
		claims.Nickname = tokenUser.Username
	}
}

func fetchDiscoveryDocument(endpoint string) (*openIDDiscoveryDocument, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: discoveryRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected discovery status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var discovery openIDDiscoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&discovery); err != nil {
		return nil, err
	}
	if discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" || discovery.UserInfoEndpoint == "" {
		return nil, errors.New("discovery document missing required endpoints")
	}

	return &discovery, nil
}

func cloneSSOSettings(settings *model.SSOSettings) *model.SSOSettings {
	if settings == nil {
		return nil
	}

	cloned := *settings
	return &cloned
}

func needsDiscovery(settings *model.SSOSettings) bool {
	return stringValue(settings.DiscoveryEndpoint) != "" &&
		(stringValue(settings.AuthEndpoint) == "" || stringValue(settings.TokenEndpoint) == "" || stringValue(settings.UserAPIEndpoint) == "")
}

func stripDomain(value string) string {
	if idx := strings.Index(value, "@"); idx >= 0 {
		return value[:idx]
	}
	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func authDataValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func usernameLogger(rctx request.CTX) mlog.LoggerIFace {
	if rctx != nil {
		return rctx.Logger()
	}

	logger, err := mlog.NewLogger()
	if err != nil {
		return mlog.CreateConsoleLogger()
	}
	return logger
}
