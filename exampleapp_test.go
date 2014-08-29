package appenginetesting

import (
	"net/http"
	"path/filepath"
	"testing"

	"appengine"
	"appengine/datastore"

	"github.com/mzimmerman/appenginetesting/exampleapp"
)

func TestTemplates(t *testing.T) {
	// Create mocked context
	c, err := NewContext(&Options{
		AppId:   "exampleapp", // appid must be used since custom.yaml specifies an application id
		Testing: t,
		Debug:   LogChild,
		Modules: []ModuleConfig{
			{
				Name: "exampleapp",
				Path: filepath.Join("exampleapp/app.yaml"),
			},
		},
	})
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	defer c.Close()

	// create data in your system
	u := exampleapp.User{Name: "Alice"}
	_, err = datastore.Put(c, datastore.NewIncompleteKey(c, "User", nil), &u)
	if err != nil {
		t.Fatalf("Error on put - %v", err)
	}
	u = exampleapp.User{Name: "Bob"}
	_, err = datastore.Put(c, datastore.NewIncompleteKey(c, "User", nil), &u)
	if err != nil {
		t.Fatalf("Error on put - %v", err)
	}

	var templates = []struct {
		name string // input
		code int    // expected result
	}{

		{"/", 200},
		{"/missing", 404},
	}
	defHost, err := appengine.ModuleHostname(c, "exampleapp", "", "")
	if err != nil {
		t.Error(err)
	}
	for _, testPage := range templates {
		resp, err := http.Get("http://" + defHost + testPage.name)
		if err != nil {
			t.Errorf("Error fetching page - %s - %v", testPage.name, err)
		} else if resp.StatusCode != testPage.code {
			t.Errorf("Fetched page %s, expected %d, got %d", testPage.name, testPage.code, resp.StatusCode)
		}
	}
}
