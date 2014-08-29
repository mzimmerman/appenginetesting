package appenginetesting

import (
	"text/template"
)

const helperTemplString = `
package {{.}}

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"appengine"
)

func init() {
	http.HandleFunc("/info", info)
	http.HandleFunc("/call", call)
}

func info(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	found := false
	for i, a := range os.Args {
		if a == "-addr_api" {
			found = true
			log.Printf("FAKE_APP_API_SOCKET:%s", os.Args[i+1])
		}
	}
	if !found {
		http.Error(w, "socket not found", 404)
	}
}

type fakeProto struct {
	data []byte
}

func (p *fakeProto) Reset() {
    *p = fakeProto{}
}

func (p *fakeProto) String() string {
    return string(p.data)
}

func (*fakeProto) ProtoMessage() {
}

func (p *fakeProto) Marshal() ([]byte, error) {
        return p.data, nil
}

func (p *fakeProto) Unmarshal(data []byte) error {
        p.data = make([]byte, len(data))
        copy(p.data, data)
        return nil
}

func call(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	body, err := ioutil.ReadAll(r.Body)
	service, method := r.FormValue("s"), r.FormValue("m")
	//c.Debugf("making API call for %q.%q ; body len = %d (cl=%d), %v", service, method, len(body), r.ContentLength, err)
	if err != nil {
		http.Error(w, "failed to read body", 500)
		return
	}
	in := &fakeProto{body}
	out := &fakeProto{}
	// mzimmerman: the following is obviously not the way to support Namespaces, however I didn't know a way to fix it
	if method == "GetNamespace" {
		return
	}
	err = c.Call(service, method, in, out, nil)
	//c.Debugf("API call %q.%q = %v", service, method, err)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/x-proto")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(out.data)))
	w.Write(out.data)
}
`

var helperTempl = template.Must(template.New("helper.go").Parse(helperTemplString))
