package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ArgoApp is the Argo CD Application that installs Matinee, so Settings →
// Updates can move it to another version. Argo CD then renders the chart
// from that revision and rolls the new pod out, as for any change in git.
type ArgoApp struct {
	Client          *Client
	Namespace, Name string
}

// ImageTagParam is the chart value that picks Matinee's image.
const ImageTagParam = "image.tag"

// Source is where the Application installs the chart from.
type Source struct {
	TargetRevision string `json:"targetRevision"`
	ImageTag       string `json:"imageTag,omitempty"`
}

func (a *ArgoApp) path() string {
	return "/apis/argoproj.io/v1alpha1/namespaces/" + url.PathEscape(a.Namespace) + "/applications/" + url.PathEscape(a.Name)
}

func (a *ArgoApp) get(ctx context.Context) (map[string]any, error) {
	status, body, err := a.Client.request(ctx, http.MethodGet, a.path(), "", nil)
	if err != nil {
		return nil, err
	}
	switch status {
	case http.StatusOK:
	case http.StatusForbidden:
		return nil, fmt.Errorf("Matinee isn't allowed to read the Argo CD Application %s/%s; set the chart's updates.argoCD values", a.Namespace, a.Name)
	case http.StatusNotFound:
		return nil, fmt.Errorf("there's no Argo CD Application %s/%s", a.Namespace, a.Name)
	default:
		return nil, fmt.Errorf("read the Argo CD Application: HTTP %d", status)
	}
	var app map[string]any
	if err := json.Unmarshal(body, &app); err != nil {
		return nil, err
	}
	return app, nil
}

// source finds the Application's one source. Applications with several
// (spec.sources) are left alone: which one is Matinee's is anyone's guess.
func source(app map[string]any) (map[string]any, error) {
	spec, _ := app["spec"].(map[string]any)
	src, _ := spec["source"].(map[string]any)
	if src == nil {
		return nil, fmt.Errorf("the Argo CD Application has no single source (spec.source) to move")
	}
	return src, nil
}

func params(src map[string]any) []any {
	helm, _ := src["helm"].(map[string]any)
	list, _ := helm["parameters"].([]any)
	return list
}

// Source reads which revision and image the Application installs now.
func (a *ArgoApp) Source(ctx context.Context) (Source, error) {
	app, err := a.get(ctx)
	if err != nil {
		return Source{}, err
	}
	src, err := source(app)
	if err != nil {
		return Source{}, err
	}
	s := Source{}
	s.TargetRevision, _ = src["targetRevision"].(string)
	for _, p := range params(src) {
		if m, ok := p.(map[string]any); ok && m["name"] == ImageTagParam {
			s.ImageTag, _ = m["value"].(string)
		}
	}
	return s, nil
}

// SetSource points the Application at another revision of the chart and
// image. The rest of the Application (its values, its other parameters)
// stays as it is. It fails rather than overwrite a change made meanwhile.
func (a *ArgoApp) SetSource(ctx context.Context, s Source) error {
	app, err := a.get(ctx)
	if err != nil {
		return err
	}
	src, err := source(app)
	if err != nil {
		return err
	}
	meta, _ := app["metadata"].(map[string]any)
	list := params(src)
	out := make([]any, 0, len(list)+1)
	set := false
	for _, p := range list {
		if m, ok := p.(map[string]any); ok && m["name"] == ImageTagParam {
			if set || s.ImageTag == "" {
				continue
			}
			m["value"] = s.ImageTag
			set = true
		}
		out = append(out, p)
	}
	if !set && s.ImageTag != "" {
		out = append(out, map[string]any{"name": ImageTagParam, "value": s.ImageTag})
	}
	patch := map[string]any{
		// A precondition: the patch fails if the Application changed since it was read.
		"metadata": map[string]any{"resourceVersion": meta["resourceVersion"]},
		"spec": map[string]any{"source": map[string]any{
			"targetRevision": s.TargetRevision,
			"helm":           map[string]any{"parameters": out},
		}},
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	status, resp, err := a.Client.request(ctx, http.MethodPatch, a.path(), "application/merge-patch+json", body)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusOK:
		return nil
	case http.StatusForbidden:
		return fmt.Errorf("Matinee isn't allowed to change the Argo CD Application %s/%s; set the chart's updates.argoCD values", a.Namespace, a.Name)
	case http.StatusConflict:
		return fmt.Errorf("the Argo CD Application changed while Matinee was updating it; try again")
	}
	return fmt.Errorf("change the Argo CD Application: HTTP %d: %s", status, strings.TrimSpace(string(resp)))
}

// request is do, keeping the answer's body.
func (c *Client) request(ctx context.Context, method, path, contentType string, body []byte) (int, []byte, error) {
	var r io.Reader
	if body != nil {
		r = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	return res.StatusCode, b, err
}
