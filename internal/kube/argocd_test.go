package kube

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestArgoApp(t *testing.T) {
	app := map[string]any{
		"metadata": map[string]any{"name": "matinee", "resourceVersion": "42"},
		"spec": map[string]any{"source": map[string]any{
			"repoURL":        "https://github.com/yeixio/matinee.git",
			"targetRevision": "edge",
			"helm": map[string]any{
				"releaseName": "matinee",
				"parameters": []any{
					map[string]any{"name": "other", "value": "x"},
					map[string]any{"name": "image.tag", "value": "sha-$ARGOCD_APP_REVISION_SHORT"},
				},
			},
		}},
	}
	var patch map[string]any
	var contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/argoproj.io/v1alpha1/namespaces/argocd/applications/matinee" || r.Header.Get("Authorization") != "Bearer t" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPatch {
			contentType = r.Header.Get("Content-Type")
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &patch)
		}
		json.NewEncoder(w).Encode(app)
	}))
	defer srv.Close()
	a := &ArgoApp{Client: &Client{BaseURL: srv.URL, Token: "t", HTTP: srv.Client()}, Namespace: "argocd", Name: "matinee"}
	ctx := context.Background()

	src, err := a.Source(ctx)
	if err != nil || src != (Source{TargetRevision: "edge", ImageTag: "sha-$ARGOCD_APP_REVISION_SHORT"}) {
		t.Fatalf("Source: %+v %v", src, err)
	}
	if err := a.SetSource(ctx, Source{TargetRevision: "v0.2.3", ImageTag: "0.2.3"}); err != nil {
		t.Fatal(err)
	}
	if contentType != "application/merge-patch+json" {
		t.Errorf("content type %s", contentType)
	}
	b, _ := json.Marshal(patch)
	want := `{"metadata":{"resourceVersion":"42"},"spec":{"source":{"helm":{"parameters":[{"name":"other","value":"x"},{"name":"image.tag","value":"0.2.3"}]},"targetRevision":"v0.2.3"}}}`
	if string(b) != want {
		t.Errorf("patch\n got %s\nwant %s", b, want)
	}

	// Without an image.tag parameter, one is added.
	app["spec"].(map[string]any)["source"].(map[string]any)["helm"] = map[string]any{}
	a.SetSource(ctx, Source{TargetRevision: "abc", ImageTag: "sha-abc"})
	b, _ = json.Marshal(patch["spec"])
	if string(b) != `{"source":{"helm":{"parameters":[{"name":"image.tag","value":"sha-abc"}]},"targetRevision":"abc"}}` {
		t.Errorf("added: %s", b)
	}

	// Several sources aren't touched.
	app["spec"] = map[string]any{"sources": []any{}}
	if err := a.SetSource(ctx, Source{TargetRevision: "x"}); err == nil {
		t.Error("changed an Application with several sources")
	}
}
