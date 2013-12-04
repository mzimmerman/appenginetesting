// +build !appengine

// Copyright 2011 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

// This file changed by Takuya Ueda from http://code.google.com/p/gae-go-testing/.

// Package appenginetesting provides an appengine.Context for testing.
package appenginetesting

import (
	"bufio"
	"bytes"
	"code.google.com/p/goprotobuf/proto"
	"errors"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"runtime"

	"appengine"
	"appengine_internal"
	basepb "appengine_internal/base"
)

// Statically verify that Context implements appengine.Context.
var _ appengine.Context = (*Context)(nil)

// httpClient is used to communicate with the helper child process's
// webserver.  We can't use http.DefaultClient anymore, as it's now
// blacklisted in App Engine 1.6.1 due to people misusing it in blog
// posts and such.  (but this is one of the rare valid uses of not
// using urlfetch)
var httpClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}

var currentContext = (*Context)(nil)

// Default API Version
const DefaultAPIVersion = "go1"

// Dev app server script filename
const AppServerFileName = "dev_appserver.py"

// API version of golang.
// It is used for app.yaml of dev_server setting.
const APIVersion = DefaultAPIVersion

const appYAMLTemplString = `
application: testapp 
version: 1
runtime: go
api_version: go1

handlers:
- url: /.*
  script: _go_app
`

// Context implements appengine.Context by running a dev_appserver.py
// process as a child and proxying all Context calls to the child.
// Use NewContext to create one.
type Context struct {
	appid      string
	req        *http.Request
	child      *exec.Cmd
	port       int      // of child dev_appserver.py http server
	adminPort  int      // of child administration dev_appserver.py http server
	appDir     string   // temp dir for application files
	queues     []string // list of queues to support
	debug      string   // send the output of the application to console
	debugChild bool     // send the output of the dev_appserver to console, for debugging appenginetesting
}

func (c *Context) AppID() string {
	return c.appid
}

func (c *Context) logf(level, format string, args ...interface{}) {
	switch {
	case c.debug == level:
		fallthrough
	case c.debug == "critical" && level == "error":
		fallthrough
	case c.debug == "warning" && (level == "critical" || level == "error"):
		fallthrough
	case c.debug == "info" && (level == "warning" || level == "critical" || level == "error"):
		fallthrough
	case c.debug == "debug" && (level == "info" || level == "warning" || level == "critical" || level == "error"):
		fallthrough
	case c.debug == "child":
		log.Printf(strings.ToUpper(level)+": "+format, args...)
		//default:
		//	log.Printf("NOTLOGGED: "+level+": "+format, args...)
	}
}

func (c *Context) Debugf(format string, args ...interface{})    { c.logf("debug", format, args...) }
func (c *Context) Infof(format string, args ...interface{})     { c.logf("info", format, args...) }
func (c *Context) Warningf(format string, args ...interface{})  { c.logf("warning", format, args...) }
func (c *Context) Criticalf(format string, args ...interface{}) { c.logf("critical", format, args...) }
func (c *Context) Errorf(format string, args ...interface{})    { c.logf("error", format, args...) }

func (c *Context) GetCurrentNamespace() string {
	return c.req.Header.Get("X-AppEngine-Current-Namespace")
}

func (c *Context) CurrentNamespace(namespace string) {
	c.req.Header.Set("X-AppEngine-Current-Namespace", namespace)
}

func (c *Context) Login(email string, admin bool) {
	c.req.Header.Add("X-AppEngine-Internal-User-Email", email)
	c.req.Header.Add("X-AppEngine-Internal-User-Id", strconv.Itoa(int(crc32.Checksum([]byte(email), crc32.IEEETable))))
	c.req.Header.Add("X-AppEngine-Internal-User-Federated-Identity", email)
	if admin {
		c.req.Header.Add("X-AppEngine-Internal-User-Is-Admin", "1")
	} else {
		c.req.Header.Add("X-AppEngine-Internal-User-Is-Admin", "0")
	}
}

func (c *Context) Logout() {
	c.req.Header.Del("X-AppEngine-Internal-User-Email")
	c.req.Header.Del("X-AppEngine-Internal-User-Id")
	c.req.Header.Del("X-AppEngine-Internal-User-Is-Admin")
	c.req.Header.Del("X-AppEngine-Internal-User-Federated-Identity")
}

func (c *Context) Call(service, method string, in, out appengine_internal.ProtoMessage, opts *appengine_internal.CallOptions) error {
	if service == "__go__" {
		if method == "GetNamespace" {
			out.(*basepb.StringProto).Value = proto.String(c.req.Header.Get("X-AppEngine-Current-Namespace"))
			return nil
		}
		if method == "GetDefaultNamespace" {
			out.(*basepb.StringProto).Value = proto.String(c.req.Header.Get("X-AppEngine-Default-Namespace"))
			return nil
		}
	}
	cn := c.GetCurrentNamespace()
	if cn != "" {
		if mod, ok := appengine_internal.NamespaceMods[service]; ok {
			mod(in, cn)
		}
	}
	data, err := proto.Marshal(in)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("http://127.0.0.1:%d/call?s=%s&m=%s", c.port, service, method),
		bytes.NewBuffer(data))
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		body, _ := ioutil.ReadAll(res.Body)
		return fmt.Errorf("got status %d; body: %q", res.StatusCode, body)
	}
	pbytes, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return err
	}
	return proto.Unmarshal(pbytes, out)
}

func (c *Context) FullyQualifiedAppID() string {
	// TODO(bradfitz): is this right, prepending "dev~"?  It at
	// least appears to make the Python datastore fake happy.
	return "dev~" + c.appid
}

func (c *Context) Request() interface{} {
	return c.req
}

// Close kills the child dev_appserver.py process, releasing its
// resources.
//
// Close is not part of the appengine.Context interface.
func (c *Context) Close() {
	if c == nil || c.child == nil {
		return
	}
	if p := c.child.Process; p != nil {
		p.Signal(syscall.SIGTERM)
	}
	os.RemoveAll(c.appDir)
	c.child = nil
	currentContext = nil
}

// Options control optional behavior for NewContext.
type Options struct {
	// AppId to pretend to be. By default, "testapp"
	AppId      string
	TaskQueues []string
	Debug      string
	DebugChild bool
}

func (o *Options) appId() string {
	if o == nil || o.AppId == "" {
		return "testapp"
	}
	return o.AppId
}

func (o *Options) taskQueues() []string {
	if o == nil || len(o.TaskQueues) == 0 {
		return []string{}
	}
	return o.TaskQueues
}

func (o *Options) debug() string {
	if o == nil || o.Debug == "" {
		return "error"
	}
	return o.Debug
}

func (o *Options) debugChild() bool {
	if o == nil {
		return false
	}
	return o.DebugChild
}

func findFreePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	addr := ln.Addr().(*net.TCPAddr)
	return addr.Port, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findDevAppserver() (string, error) {
	if e := os.Getenv("APPENGINE_SDK"); e != "" {
		p := filepath.Join(e, AppServerFileName)
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("invalid APPENGINE_SDK environment variable; path %q doesn't exist", p)
	}
	try := []string{
		filepath.Join(os.Getenv("HOME"), "sdk", "go_appengine", AppServerFileName),
		filepath.Join(os.Getenv("HOME"), "sdk", "google_appengine", AppServerFileName),
		filepath.Join(os.Getenv("HOME"), "google_appengine", AppServerFileName),
		filepath.Join(os.Getenv("HOME"), "go_appengine", AppServerFileName),
	}
	for _, p := range try {
		if fileExists(p) {
			return p, nil
		}
	}
	return exec.LookPath(AppServerFileName)
}

func (c *Context) startChild() error {

	port, err := findFreePort()
	if err != nil {
		return err
	}
	adminPort, err := findFreePort()
	if err != nil {
		return err
	}

	c.appDir, err = ioutil.TempDir("", "")
	if err != nil {
		return err
	}

	if len(c.queues) > 0 {
		queueBuf := new(bytes.Buffer)
		queueTempl.Execute(queueBuf, c.queues)
		err = ioutil.WriteFile(filepath.Join(c.appDir, "queue.yaml"), queueBuf.Bytes(), 0755)
		if err != nil {
			return err
		}
	}

	err = os.Mkdir(filepath.Join(c.appDir, "helper"), 0755)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(filepath.Join(c.appDir, "app.yaml"), []byte(appYAMLTemplString), 0755)
	if err != nil {
		return err
	}

	helperBuf := new(bytes.Buffer)
	helperTempl.Execute(helperBuf, nil)
	err = ioutil.WriteFile(filepath.Join(c.appDir, "helper", "helper.go"), helperBuf.Bytes(), 0644)
	if err != nil {
		return err
	}
	devAppserver, err := findDevAppserver()
	c.port = port
	c.adminPort = adminPort
	devServerLog := "info"
	appLog := c.debug
	if c.debug == "child" {
		devServerLog = "debug"
		appLog = "debug"
	}
	
	switch runtime.GOOS {
		case "windows":
			c.child = exec.Command(
				"cmd",
				"/C",
				devAppserver,
				"--clear_datastore=yes",
				"--skip_sdk_update_check=yes",
				fmt.Sprintf("--storage_path=%s/data.datastore", c.appDir),
				fmt.Sprintf("--log_level=%s", appLog),
				fmt.Sprintf("--dev_appserver_log_level=%s", devServerLog),
				fmt.Sprintf("--port=%d", port),
				fmt.Sprintf("--admin_port=%d", adminPort),
				c.appDir,
			)
		case "linux":
		case "darwin":
			c.child = exec.Command(
				devAppserver,
				"--clear_datastore=yes",
				"--skip_sdk_update_check=yes",
				fmt.Sprintf("--storage_path=%s/data.datastore", c.appDir),
				fmt.Sprintf("--log_level=%s", appLog),
				fmt.Sprintf("--dev_appserver_log_level=%s", devServerLog),
				fmt.Sprintf("--port=%d", port),
				fmt.Sprintf("--admin_port=%d", adminPort),
				c.appDir,
			)
	}
	
	stderr, err := c.child.StderrPipe()
	if err != nil {
		return err
	}

	err = c.child.Start()
	if err != nil {
		return err
	}

	r := bufio.NewReader(stderr)
	donec := make(chan bool)
	errc := make(chan error)
	go func() {
		done := false
		for {
			bs, err := r.ReadSlice('\n')
			if err != nil {
				errc <- err
				return
			}
			line := string(bs)
			c.logf("CHILD", "%q", line)
			if done {
				continue
			}
			if strings.Contains(line, "Starting admin server at") {
				done = true
				donec <- true
			}
		}
	}()
	select {
	case err := <-errc:
		return fmt.Errorf("error starting child process: %v", err)
	case <-time.After(10e9):
		if p := c.child.Process; p != nil {
			p.Kill()
		}
		return errors.New("timeout starting process")
	case <-donec:
	}

	return nil
}

// NewContext returns a new AppEngine context with an empty datastore, etc.
// A nil Options is valid and means to use the default values.
func NewContext(opts *Options) (*Context, error) {
	if currentContext != nil {
		return nil, errors.New("Previous Context was not closed!")
	}
	req, _ := http.NewRequest("GET", "/", nil)
	c := &Context{
		appid:      opts.appId(),
		req:        req,
		queues:     opts.taskQueues(),
		debug:      opts.debug(),
		debugChild: opts.debugChild(),
	}
	if err := c.startChild(); err != nil {
		return nil, err
	}
	currentContext = c
	return c, nil
}
