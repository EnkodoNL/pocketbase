package apis_test

import (
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/subscriptions"
	"ganigeorgiev.com/pocketbase/pocketbase/ui"
)

func TestRecordAuthWithOAuth2Redirect(t *testing.T) {
	t.Parallel()

	clientStubs := make([]map[string]subscriptions.Client, 0, 16)

	for range 16 {
		c1 := subscriptions.NewDefaultClient()

		c2 := subscriptions.NewDefaultClient()
		c2.Subscribe("@oauth2")

		c3 := subscriptions.NewDefaultClient()
		c3.Subscribe("test1", "@oauth2")

		c4 := subscriptions.NewDefaultClient()
		c4.Subscribe("test1", "test2")

		c5 := subscriptions.NewDefaultClient()
		c5.Subscribe("@oauth2")
		c5.Discard()

		clientStubs = append(clientStubs, map[string]subscriptions.Client{
			"c1": c1,
			"c2": c2,
			"c3": c3,
			"c4": c4,
			"c5": c5,
		})
	}

	// note: don't exit because it is usually called as part of a separate goroutine
	checkClientMessages := func(t testing.TB, clientId string, msg subscriptions.Message, expectedMessages map[string][]string) {
		if len(expectedMessages[clientId]) == 0 {
			t.Errorf("Unexpected client %q message, got %q:\n%q", clientId, msg.Name, msg.Data)
			return
		}

		if msg.Name != "@oauth2" {
			t.Errorf("Expected @oauth2 msg.Name, got %q", msg.Name)
			return
		}

		for _, txt := range expectedMessages[clientId] {
			if !strings.Contains(string(msg.Data), txt) {
				t.Errorf("Failed to find %q in \n%s", txt, msg.Data)
				return
			}
		}
	}

	beforeTestFunc := func(
		clients map[string]subscriptions.Client,
		expectedMessages map[string][]string,
	) func(testing.TB, *tests.TestApp, *core.ServeEvent) {
		return func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			for _, client := range clients {
				app.SubscriptionsBroker().Register(client)
			}

			ctx, cancelFunc := context.WithTimeout(context.Background(), 100*time.Millisecond)

			// add to the app store so that it can be cancelled manually after test completion
			app.Store().Set("cancelFunc", cancelFunc)

			go func() {
				defer cancelFunc()

				for {
					select {
					case msg, ok := <-clients["c1"].Channel():
						if ok {
							checkClientMessages(t, "c1", msg, expectedMessages)
						} else {
							t.Errorf("Unexpected c1 closed channel")
						}
					case msg, ok := <-clients["c2"].Channel():
						if ok {
							checkClientMessages(t, "c2", msg, expectedMessages)
						} else {
							t.Errorf("Unexpected c2 closed channel")
						}
					case msg, ok := <-clients["c3"].Channel():
						if ok {
							checkClientMessages(t, "c3", msg, expectedMessages)
						} else {
							t.Errorf("Unexpected c3 closed channel")
						}
					case msg, ok := <-clients["c4"].Channel():
						if ok {
							checkClientMessages(t, "c4", msg, expectedMessages)
						} else {
							t.Errorf("Unexpected c4 closed channel")
						}
					case _, ok := <-clients["c5"].Channel():
						if ok {
							t.Errorf("Expected c5 channel to be closed")
						}
					case <-ctx.Done():
						for _, c := range clients {
							c.Discard()
						}
						return
					}
				}
			}()
		}
	}

	uiFS := ui.DistDirFS

	scenarios := []tests.ApiScenario{
		{
			Name:   "no state query param",
			Method: http.MethodGet,
			URL:    "/api/oauth2-redirect?code=123",
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				beforeTestFunc(clientStubs[0], nil)(t, app, e)

				ui.DistDirFS = nil // mock at least once no_ui build tag
			},
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			NotExpectedContent: []string{
				`<link`,
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				ui.DistDirFS = uiFS // restore
				app.Store().Get("cancelFunc").(context.CancelFunc)()
			},
		},
		{
			Name:           "invalid or missing client",
			Method:         http.MethodGet,
			URL:            "/api/oauth2-redirect?code=123&state=missing",
			BeforeTestFunc: beforeTestFunc(clientStubs[1], nil),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
				`<link rel="stylesheet" type="text/css" href="/_/assets/index-`,
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()
			},
		},
		{
			Name:   "no code query param",
			Method: http.MethodGet,
			URL:    "/api/oauth2-redirect?state=" + clientStubs[2]["c3"].Id(),
			BeforeTestFunc: beforeTestFunc(clientStubs[2], map[string][]string{
				"c3": {`"state":"` + clientStubs[2]["c3"].Id(), `"code":""`},
			}),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[2]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
		{
			Name:   "error query param",
			Method: http.MethodGet,
			URL:    "/api/oauth2-redirect?error=example&code=123&state=" + clientStubs[3]["c3"].Id(),
			BeforeTestFunc: beforeTestFunc(clientStubs[3], map[string][]string{
				"c3": {`"state":"` + clientStubs[3]["c3"].Id(), `"code":"123"`, `"error":"example"`},
			}),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[3]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
		{
			Name:           "discarded client with @oauth2 subscription",
			Method:         http.MethodGet,
			URL:            "/api/oauth2-redirect?code=123&state=" + clientStubs[4]["c5"].Id(),
			BeforeTestFunc: beforeTestFunc(clientStubs[4], nil),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()
			},
		},
		{
			Name:           "client without @oauth2 subscription",
			Method:         http.MethodGet,
			URL:            "/api/oauth2-redirect?code=123&state=" + clientStubs[5]["c4"].Id(),
			BeforeTestFunc: beforeTestFunc(clientStubs[5], nil),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()
			},
		},
		{
			Name:   "client with @oauth2 subscription",
			Method: http.MethodGet,
			URL:    "/api/oauth2-redirect?code=123&state=" + clientStubs[6]["c3"].Id(),
			BeforeTestFunc: beforeTestFunc(clientStubs[6], map[string][]string{
				"c3": {`"state":"` + clientStubs[6]["c3"].Id(), `"code":"123"`},
			}),
			ExpectedStatus: 200,
			ExpectedContent: []string{
				"Authentication completed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[6]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
		{
			Name:   "(POST) client with @oauth2 subscription",
			Method: http.MethodPost,
			URL:    "/api/oauth2-redirect",
			Body:   strings.NewReader("code=123&state=" + clientStubs[7]["c3"].Id()),
			Headers: map[string]string{
				"content-type": "application/x-www-form-urlencoded",
			},
			BeforeTestFunc: beforeTestFunc(clientStubs[7], map[string][]string{
				"c3": {`"state":"` + clientStubs[7]["c3"].Id(), `"code":"123"`},
			}),
			ExpectedStatus: 200,
			ExpectedContent: []string{
				"Authentication completed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[7]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
		{
			Name:   "(POST) Apple user's name json (nameKey error)",
			Method: http.MethodPost,
			URL:    "/api/oauth2-redirect",
			Body: strings.NewReader(url.Values{
				"code":  []string{strings.Repeat("a", 986)},
				"state": []string{clientStubs[8]["c3"].Id()},
				"user": []string{
					`{"name":{"firstName":"aaa","lastName":"` + strings.Repeat("b", 200) + `"}}`,
				},
			}.Encode()),
			Headers: map[string]string{
				"content-type": "application/x-www-form-urlencoded",
			},
			BeforeTestFunc: beforeTestFunc(clientStubs[8], map[string][]string{
				"c3": {`"state":"` + clientStubs[8]["c3"].Id(), `"code":"` + strings.Repeat("a", 986) + `"`},
			}),
			ExpectedStatus: 200,
			ExpectedContent: []string{
				"Authentication completed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[8]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}

				if storedName := app.Store().Get("@redirect_name_" + strings.Repeat("a", 986)); storedName != nil {
					t.Fatalf("Didn't expect stored name, got %q", storedName)
				}
			},
		},
		{
			Name:   "(POST) Apple user's name json",
			Method: http.MethodPost,
			URL:    "/api/oauth2-redirect",
			Body: strings.NewReader(url.Values{
				"code":  []string{strings.Repeat("a", 985)},
				"state": []string{clientStubs[9]["c3"].Id()},
				"user": []string{
					`{"name":{"firstName":"aaa","lastName":"` + strings.Repeat("b", 200) + `"}}`,
				},
			}.Encode()),
			Headers: map[string]string{
				"content-type": "application/x-www-form-urlencoded",
			},
			BeforeTestFunc: beforeTestFunc(clientStubs[9], map[string][]string{
				"c3": {`"state":"` + clientStubs[9]["c3"].Id(), `"code":"` + strings.Repeat("a", 985) + `"`},
			}),
			ExpectedStatus: 200,
			ExpectedContent: []string{
				"Authentication completed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[9]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}

				storedName, _ := app.Store().Get("@redirect_name_" + strings.Repeat("a", 985)).(string)
				expectedName := "aaa " + strings.Repeat("b", 146)
				if storedName != expectedName {
					t.Fatalf("Expected stored name\n%q\ngot\n%q", expectedName, storedName)
				}
			},
		},

		// -----------------------------------------------------------
		// Redirect confirmation
		// -----------------------------------------------------------

		{
			Name:    "redirect confirmation - client with different IP",
			Method:  http.MethodGet,
			URL:     "/api/oauth2-redirect?code=123&state=" + clientStubs[10]["c3"].Id(),
			Headers: map[string]string{"x-test-ip": "127.0.0.2"},
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				app.Settings().TrustedProxy.Headers = []string{"x-test-ip"}

				clientStubs[10]["c3"].Set(apis.RealtimeClientIPKey, "127.0.0.1")

				beforeTestFunc(clientStubs[10], map[string][]string{
					"c3": {`"state":"` + clientStubs[10]["c3"].Id(), `"code":"123"`},
				})(t, app, e)
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				"Confirm authentication",
				`<form action="/api/oauth2-redirect/confirm" method="POST"`,
				`<input type="hidden" name="state" value="` + clientStubs[10]["c3"].Id() + `">`,
				`<button type="submit" class="btn lg block transparent danger">Cancel authentication</button>`,
				`<button type="submit" name="key" value="`,
				`>Confirm authentication</button>`,
			},
			NotExpectedContent: []string{
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				// app.Store().Get("cancelFunc").(context.CancelFunc)()

				if !clientStubs[10]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to not be removed")
				}

				body, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				res.Body.Close()

				// extract the random key from the html
				matches := regexp.MustCompile(`name="key" value="(.*?)"`).FindStringSubmatch(string(body))
				if len(matches) != 2 {
					t.Fatal("Expected body to match key input regex")
				}
				key := matches[1]

				dataRaw, _ := json.Marshal(clientStubs[10]["c3"].Get(key))
				dataStr := string(dataRaw)

				expectedParts := []string{
					`"state":"` + clientStubs[10]["c3"].Id() + `"`,
					`"code":"123"`,
				}
				for _, part := range expectedParts {
					if !strings.Contains(dataStr, part) {
						t.Fatalf("Expected to find %q in\n%s", part, dataStr)
					}
				}
			},
		},
		{
			Name:   "redirect confirmation - confirm",
			Method: http.MethodPost,
			URL:    "/api/oauth2-redirect/confirm",
			Body:   strings.NewReader(`{"state":"` + clientStubs[11]["c3"].Id() + `","key":"abc"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				clientStubs[11]["c3"].Set("abc", apis.OAuth2RedirectData{Code: "test"})

				beforeTestFunc(clientStubs[11], map[string][]string{
					"c3": {`"code":"test"`},
				})(t, app, e)
			},
			ExpectedStatus: 200,
			ExpectedContent: []string{
				"Authentication completed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[11]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
		{
			Name:   "redirect confirmation - cancel",
			Method: http.MethodPost,
			URL:    "/api/oauth2-redirect/confirm",
			Body:   strings.NewReader(`{"state":"` + clientStubs[12]["c3"].Id() + `"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				clientStubs[12]["c3"].Set("abc", apis.OAuth2RedirectData{Code: "test"})

				beforeTestFunc(clientStubs[12], map[string][]string{
					"c3": {`"state":"` + clientStubs[12]["c3"].Id() + `"`, `"error":"Cancelled authentication.`},
				})(t, app, e)
			},
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[12]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
		{
			Name:           "redirect confirmation - client with discarded @oauth2 subscription",
			Method:         http.MethodPost,
			URL:            "/api/oauth2-redirect/confirm",
			Body:           strings.NewReader(`{"state":"` + clientStubs[13]["c5"].Id() + `"}`),
			BeforeTestFunc: beforeTestFunc(clientStubs[13], nil),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()
			},
		},
		{
			Name:           "redirect confirmation - missing client",
			Method:         http.MethodPost,
			URL:            "/api/oauth2-redirect/confirm",
			Body:           strings.NewReader(`{"state":"missing"}`),
			BeforeTestFunc: beforeTestFunc(clientStubs[14], nil),
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
		},
		{
			Name:   "redirect confirmation - confirm (different key)",
			Method: http.MethodPost,
			URL:    "/api/oauth2-redirect/confirm",
			Body:   strings.NewReader(`{"state":"` + clientStubs[15]["c3"].Id() + `","key":"invalid"}`),
			BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
				clientStubs[15]["c3"].Set("abc", apis.OAuth2RedirectData{Code: "test"})

				beforeTestFunc(clientStubs[15], map[string][]string{
					"c3": {`"state":"` + clientStubs[15]["c3"].Id() + `"`, `"error":"Missing or invalid OAuth2 client data."`},
				})(t, app, e)
			},
			ExpectedStatus: 400,
			ExpectedContent: []string{
				"Authentication failed",
				"<script>window.close()</script>",
			},
			ExpectedEvents: map[string]int{"*": 0},
			AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
				app.Store().Get("cancelFunc").(context.CancelFunc)()

				if clientStubs[15]["c3"].HasSubscription("@oauth2") {
					t.Fatalf("Expected oauth2 subscription to be removed")
				}
			},
		},
	}

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
