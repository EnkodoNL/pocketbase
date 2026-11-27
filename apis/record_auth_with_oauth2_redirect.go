package apis

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis/internal/systemui"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/security"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/subscriptions"
)

const (
	oauth2SubscriptionTopic               string = "@oauth2"
	oauth2RedirectAppleNameStoreKeyPrefix string = "@redirect_name_"
)

type OAuth2RedirectData struct {
	State string `form:"state" json:"state"`
	Code  string `form:"code" json:"code"`
	Error string `form:"error" json:"error,omitempty"`

	// returned by Apple only
	AppleUser string `form:"user" json:"-"`
}

func oauth2RedirectHandler(e *core.RequestEvent) error {
	data := OAuth2RedirectData{}

	if e.Request.Method == http.MethodPost {
		if err := e.BindBody(&data); err != nil {
			e.App.Logger().Warn("Failed to read OAuth2 redirect data", "error", err, "userIP", e.RealIP())
			return oauth2FailureRedirect(e)
		}
	} else {
		query := e.Request.URL.Query()
		data.State = query.Get("state")
		data.Code = query.Get("code")
		data.Error = query.Get("error")
	}

	if data.State == "" {
		e.App.Logger().Warn("Missing OAuth2 state parameter", "userIP", e.RealIP())
		return oauth2FailureRedirect(e)
	}

	client, err := e.App.SubscriptionsBroker().ClientById(data.State)
	if err != nil || client.IsDiscarded() || !client.HasSubscription(oauth2SubscriptionTopic) {
		e.App.Logger().Warn("Missing or invalid OAuth2 subscription client", "error", err, "clientId", data.State, "userIP", e.RealIP())
		return oauth2FailureRedirect(e)
	}

	if data.Code != "" && data.Error == "" {
		// temporary store the Apple user's name so that it can be later retrieved with the authWithOAuth2 call
		// (see https://ganigeorgiev.com/pocketbase/discussions/7090)
		if data.AppleUser != "" {
			nameErr := parseAndStoreAppleRedirectName(
				e.App,
				oauth2RedirectAppleNameStoreKeyPrefix+data.Code,
				data.AppleUser,
			)
			if nameErr != nil {
				// non-critical error
				e.App.Logger().Warn("Failed to parse and load Apple Redirect name data", "error", nameErr, "userIP", e.RealIP())
			}
		}

		// ask for manual confirmation to minimize the risk of XSRF attack vectors
		//
		// note: custom registered clients (aka. those without RealtimeClientIPKey in the store)
		// are excluded from the check for backward compatibility
		connectionIP, _ := client.Get(RealtimeClientIPKey).(string)
		if connectionIP != "" && e.RealIP() != connectionIP {
			key := security.RandomString(50)
			client.Set(key, data)
			return oauth2ConfirmationRedirect(e, data.State, key, connectionIP)
		}
	}

	return endOAuth2RedirectClient(e, client, data)
}

func oauth2RedirectConfirmHandler(e *core.RequestEvent) error {
	reqData := &struct {
		State string `form:"state" json:"state"`
		Key   string `form:"key" json:"key"` // should be empty if "cancelled"
	}{}

	if err := e.BindBody(reqData); err != nil {
		e.App.Logger().Warn("Failed to read OAuth2 redirect confirm data", "error", err)
		return oauth2FailureRedirect(e)
	}

	client, err := e.App.SubscriptionsBroker().ClientById(reqData.State)
	if err != nil || client.IsDiscarded() || !client.HasSubscription(oauth2SubscriptionTopic) {
		e.App.Logger().Warn("Missing or invalid OAuth2 subscription client", "error", err, "clientId", reqData.State)
		return oauth2FailureRedirect(e)
	}

	var clientData OAuth2RedirectData
	clientData.State = reqData.State

	err = checkRateLimit(e, "@pb_oauth2_confirm_"+reqData.State, core.RateLimitRule{MaxRequests: 3, Duration: 180})
	if err != nil {
		clientData.Error = "Too many attempts, please try again later."
		e.App.Logger().Warn("OAuth2 redirect confirm - internal rate limit reached", "clientId", reqData.State, "userIP", e.RealIP())
		return endOAuth2RedirectClient(e, client, clientData)
	}

	if reqData.Key == "" {
		// cancelled
		clientData.Error = "Cancelled authentication."
	} else {
		// confirmed
		data, ok := client.Get(reqData.Key).(OAuth2RedirectData)
		if ok {
			clientData = data
		} else {
			clientData.Error = "Missing or invalid OAuth2 client data."
		}
	}

	return endOAuth2RedirectClient(e, client, clientData)
}

func endOAuth2RedirectClient(e *core.RequestEvent, client subscriptions.Client, data OAuth2RedirectData) error {
	defer client.Unsubscribe(oauth2SubscriptionTopic)

	encodedData, err := json.Marshal(data)
	if err != nil {
		e.App.Logger().Warn("Failed to marshalize OAuth2 redirect data", "error", err, "clientId", data.State)
		return oauth2FailureRedirect(e)
	}

	msg := subscriptions.Message{
		Name: oauth2SubscriptionTopic,
		Data: encodedData,
	}

	client.Send(msg)

	if data.Error != "" || data.Code == "" {
		e.App.Logger().Warn("Failed OAuth2 redirect due to an error or missing code parameter", "error", data.Error, "clientId", data.State)
		return oauth2FailureRedirect(e)
	}

	return oauth2SuccessRedirect(e)
}

func oauth2FailureRedirect(e *core.RequestEvent) error {
	html, err := systemui.RenderOAuth2RedirectFailure(systemui.OAuth2RedirectFailureData{
		PageTitle: "Authentication failed",
	})
	if err != nil {
		return e.InternalServerError("", fmt.Errorf("failed to render OAuth2 redirect failure: %w", err))
	}

	return e.HTML(http.StatusBadRequest, html)
}

func oauth2SuccessRedirect(e *core.RequestEvent) error {
	html, err := systemui.RenderOAuth2RedirectSuccess(systemui.OAuth2RedirectSuccessData{
		PageTitle: "Authentication completed",
	})
	if err != nil {
		return e.InternalServerError("", fmt.Errorf("failed to render OAuth2 redirect success: %w", err))
	}

	return e.HTML(http.StatusOK, html)
}

func oauth2ConfirmationRedirect(e *core.RequestEvent, state string, key string, connectionIP string) error {
	html, err := systemui.RenderOAuth2RedirectConfirm(systemui.OAuth2RedirectConfirmData{
		PageTitle:    "Confirm authentication",
		State:        state,
		Key:          key,
		ConnectionIP: connectionIP,
		RedirectIP:   e.RealIP(),
	})
	if err != nil {
		return e.InternalServerError("", fmt.Errorf("failed to render OAuth2 redirect confirmation: %w", err))
	}

	return e.HTML(http.StatusOK, html)
}

// parseAndStoreAppleRedirectName extracts the first and last name
// from serializedNameData and temporary store them in the app.Store.
//
// This is hacky workaround to forward safely and seamlessly the Apple
// redirect user's name back to the OAuth2 auth handler.
//
// Note that currently Apple is the only provider that behaves like this and
// for now it is unnecessary to check whether the redirect is coming from Apple or not.
//
// Ideally this shouldn't be needed and will be removed in the future
// once Apple adds a dedicated userinfo endpoint.
func parseAndStoreAppleRedirectName(app core.App, nameKey string, serializedNameData string) error {
	if serializedNameData == "" {
		return nil
	}

	// just in case to prevent storing large strings in memory
	if len(nameKey) > 1000 {
		return errors.New("nameKey is too large")
	}

	// https://developer.apple.com/documentation/signinwithapple/incorporating-sign-in-with-apple-into-other-platforms#Handle-the-response
	extracted := struct {
		Name struct {
			FirstName string `json:"firstName"`
			LastName  string `json:"lastName"`
		} `json:"name"`
	}{}
	if err := json.Unmarshal([]byte(serializedNameData), &extracted); err != nil {
		return err
	}

	fullName := extracted.Name.FirstName + " " + extracted.Name.LastName

	// truncate just in case to prevent storing large strings in memory
	if len(fullName) > 150 {
		fullName = fullName[:150]
	}

	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return nil
	}

	// store (and remove)
	app.Store().Set(nameKey, fullName)
	time.AfterFunc(1*time.Minute, func() {
		app.Store().Remove(nameKey)
	})

	return nil
}
