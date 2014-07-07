package appenginetesting

import (
	"net/http"
	"testing"

	"github.com/mzimmerman/appenginetesting/exampleapp"

	"appengine/datastore"
)

func TestTemplates(t *testing.T) {
	// Create mocked context.
	c, err := NewContext(nil)
	if err != nil {
		t.Fatalf("initilizing context: %v", err)
		return
	}

	// Close the context when we are done.
	// If there are no errors, this defer will close the "real" application
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

	// start up the real application
	baseurl, err := c.RunRealApplication("exampleapp") // can be a full path or relative path to where "goapp test" would be run
	if err != nil {
		t.Fatalf("Error starting real application: %v", err)
		return
	}
	// defer c.Close() is already registered to trigger

	var templates = []struct {
		name string // input
		code int    // expected result
	}{

		{"/", 200},
		{"/missing", 404},
	}
	for _, testPage := range templates {
		resp, err := http.Get(baseurl + testPage.name)
		if err != nil {
			t.Errorf("Error fetching page - %s - %v", testPage.name, err)
		} else if resp.StatusCode != testPage.code {
			t.Errorf("Fetched page %s, expected %d, got %d", testPage.name, testPage.code, resp.StatusCode)
		}
	}
}
