package exampleapp

import (
	"fmt"
	"net/http"

	"appengine"
	"appengine/datastore"
)

func init() {
	http.HandleFunc("/", root)
}

func root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		w.WriteHeader(404)
		return
	}
	c := appengine.NewContext(r)
	result, err := datastore.NewQuery("Users").Count(c)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Error - %v", err)
		return
	}
	fmt.Fprint(w, "Hi, you found %i Users", result)
}

type User struct {
	Name string
}
