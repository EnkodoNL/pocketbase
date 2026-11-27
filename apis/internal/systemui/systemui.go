// systemui is an internal package for special system UI pages
// (currently mostly for the "All-in-one" OAuth2 redirect handler)
// that are always available even when compiled with the "no_ui" build tag.
package systemui

import (
	"embed"
	"io/fs"
	"strings"

	"ganigeorgiev.com/pocketbase/pocketbase/tools/template"
	"ganigeorgiev.com/pocketbase/pocketbase/ui"
)

//go:embed all:templates
var templatesEmbed embed.FS

var templates = template.NewRegistry()

type pageData struct {
	PageTitle string
}

func (data pageData) IndexCSS() string {
	if ui.DistDirFS == nil {
		return ""
	}

	matches, _ := fs.Glob(ui.DistDirFS, "assets/index*.css")
	if len(matches) != 1 {
		return ""
	}

	return "/_/" + strings.TrimLeft(matches[0], "/")
}

// -------------------------------------------------------------------

type OAuth2RedirectConfirmData struct {
	pageData
	State        string
	Key          string
	RedirectIP   string
	ConnectionIP string
}

func RenderOAuth2RedirectConfirm(data OAuth2RedirectConfirmData) (string, error) {
	return templates.LoadFS(
		templatesEmbed,
		"templates/layout.html",
		"templates/oauth2RedirectConfirm.html",
	).Render(data)
}

// -------------------------------------------------------------------

type OAuth2RedirectSuccessData struct {
	pageData
}

func RenderOAuth2RedirectSuccess(data OAuth2RedirectSuccessData) (string, error) {
	return templates.LoadFS(
		templatesEmbed,
		"templates/layout.html",
		"templates/oauth2RedirectSuccess.html",
	).Render(data)
}

// -------------------------------------------------------------------

type OAuth2RedirectFailureData struct {
	pageData
}

func RenderOAuth2RedirectFailure(data OAuth2RedirectFailureData) (string, error) {
	return templates.LoadFS(
		templatesEmbed,
		"templates/layout.html",
		"templates/oauth2RedirectFailure.html",
	).Render(data)
}
