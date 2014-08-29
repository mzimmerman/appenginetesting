package custom

import (
	"fmt"

	"net/http"
)

func init() {
	http.HandleFunc("/test", test)
}

func test(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hey, it works!")
}
