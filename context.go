// +build !appengine

// Copyright 2013 Google Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

// This file changed by Takuya Ueda from http://code.google.com/p/gae-go-testing/.
// This file changed by Matt Zimmerman from http://github.com/mzimmerman/appenginetesting

// Package appenginetesting provides an appengine.Context for testing.
package appenginetesting

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"code.google.com/p/goprotobuf/proto"

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

var numRunningContexts chan bool

func init() {
	numRunningContexts = make(chan bool, 1)
	numRunningContexts <- true // load it initially
}

// Dev app server script filename
const AppServerFileName = "dev_appserver.py"

// Context implements appengine.Context by running a dev_appserver.py
// process as a child and proxying all Context calls to the child.
// Use NewContext to create one.
type Context struct {
	appid     string
	req       *http.Request
	child     *exec.Cmd
	apiURL    string   // of child dev_appserver.py http server
	adminURL  string   // of child administration dev_appserver.py http server
	moduleURL string   // of "application" http server
	appDir    string   // temp dir for application files
	queues    []string // list of queues to support
	debug     string   // send the output of the application to console
}

func (c *Context) AppID() string {
	return c.appid
}

func (c *Context) logf(level, format string, args ...interface{}) {
	switch {
	case c.debug == level:
		fallthrough
	case c.debug == LogCritical && level == LogError:
		fallthrough
	case c.debug == LogWarning && (level == LogCritical || level == LogError):
		fallthrough
	case c.debug == LogInfo && (level == LogWarning || level == LogCritical || level == LogError):
		fallthrough
	case c.debug == LogDebug && (level == LogInfo || level == LogWarning || level == LogCritical || level == LogError):
		fallthrough
	case c.debug == LogChild:
		log.Printf(strings.ToUpper(level)+": "+format, args...)
		//default:
		//	log.Printf("NOTLOGGED: "+level+": "+format, args...)
	}
}

const (
	LogChild    = "child"
	LogDebug    = "debug"
	LogInfo     = "info"
	LogWarning  = "warning"
	LogCritical = "critical"
	LogError    = "error"
)

func (c *Context) Debugf(format string, args ...interface{})    { c.logf(LogDebug, format, args...) }
func (c *Context) Infof(format string, args ...interface{})     { c.logf(LogInfo, format, args...) }
func (c *Context) Warningf(format string, args ...interface{})  { c.logf(LogWarning, format, args...) }
func (c *Context) Criticalf(format string, args ...interface{}) { c.logf(LogCritical, format, args...) }
func (c *Context) Errorf(format string, args ...interface{})    { c.logf(LogError, format, args...) }

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
		fmt.Sprintf("%s/call?s=%s&m=%s", c.moduleURL, service, method),
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
func (c *Context) Close() []byte {
	if c == nil || c.child == nil {
		return nil
	}
	defer func() {
		numRunningContexts <- true
		//fmt.Printf("Cleaning up directory because Close was called\n")
		os.RemoveAll(c.appDir)
		// load a runningContext back into the queue
	}()
	if p := c.child.Process; p != nil {
		p.Signal(syscall.SIGTERM)
		if _, err := p.Wait(); err != nil {
			log.Fatalf("Error closing devappserver - %v", err)
			return nil
		}
	}
	data, err := ioutil.ReadFile(c.appDir + "/data.datastore/datastore.db")
	if err != nil {
		log.Fatalf("Could not read data.datastore file in %s - %s", c.appDir, err.Error())
	}
	c.child = nil
	return data
}

// Options control optional behavior for NewContext.
type Options struct {
	// AppId to pretend to be. By default, "testapp"
	AppId      string
	TaskQueues []string
	Debug      string
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
		return LogError
	}
	return o.Debug
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func findDevAppserver() (string, error) {
	if p := os.Getenv("APPENGINE_DEV_APPSERVER"); p != "" {
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("invalid APPENGINE_DEV_APPSERVER environment variable; path %q doesn't exist", p)
	}
	return exec.LookPath(AppServerFileName)
}

func findPython() (path string, err error) {
	for _, name := range []string{"python2.7", "python"} {
		path, err = exec.LookPath(name)
		if err == nil {
			return
		}
	}
	return
}

var apiServerAddrRE = regexp.MustCompile(`Starting API server at: (\S+)`)
var adminServerAddrRE = regexp.MustCompile(`Starting admin server at: (\S+)`)
var moduleServerAddrRE = regexp.MustCompile(`Starting module "default" running at: (\S+)`)
var logLevels = regexp.MustCompile(`^((DEBUG)|(INFO)|(WARNING)|(CRITICAL)|(ERROR))`)

func (c *Context) startChild() error {
	select {
	case <-numRunningContexts:
	default:
		return fmt.Errorf("appenginetesting already running, make sure to call Close()")
	}
	var err error
	defer func() {
		if err != nil {
			// load the queue again if we fail to start a context
			numRunningContexts <- true
		}
	}()
	var python string
	python, err = findPython()
	if err != nil {
		return fmt.Errorf("Could not find python interpreter: %v", err)
	}
	c.appDir, err = ioutil.TempDir("", "appenginetesting")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			// cleanup directory if there's an error in any of the steps following the creation of the child
			fmt.Printf("Cleaning up directory because of an error - %v\n", err)
			os.RemoveAll(c.appDir)
		}
	}()

	if len(c.queues) > 0 {
		queueBuf := new(bytes.Buffer)
		queueTempl.Execute(queueBuf, c.queues)
		err = ioutil.WriteFile(filepath.Join(c.appDir, "queue.yaml"), queueBuf.Bytes(), 0755)
		if err != nil {
			return fmt.Errorf("Error generating queue.yaml - %v", err)
		}
	}

	err = os.Mkdir(filepath.Join(c.appDir, "helper"), 0755)
	if err != nil {
		return err
	}
	appBuf := new(bytes.Buffer)
	appTempl.Execute(appBuf, c.AppID())
	err = ioutil.WriteFile(filepath.Join(c.appDir, "app.yaml"), appBuf.Bytes(), 0755)
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
	if err != nil {
		return err
	}

	appLog := c.debug
	if c.debug == LogChild {
		appLog = LogDebug
	}

	switch runtime.GOOS {
	case "windows":
		c.child = exec.Command(
			"cmd",
			"/C",
			python,
			devAppserver,
			"--clear_datastore=true",
			"--skip_sdk_update_check=true",
			fmt.Sprintf("--storage_path=%s/data.datastore", c.appDir),
			fmt.Sprintf("--log_level=%s", appLog),
			"--dev_appserver_log_level=debug",
			"--port=0",
			"--api_port=0",
			"--admin_port=0",
			c.appDir,
		)
	case "linux":
		fallthrough
	case "darwin":
		c.child = exec.Command(
			python,
			devAppserver,
			"--clear_datastore=true",
			"--skip_sdk_update_check=true",
			fmt.Sprintf("--storage_path=%s/data.datastore", c.appDir),
			fmt.Sprintf("--log_level=%s", appLog),
			"--dev_appserver_log_level=debug",
			"--port=0",
			"--api_port=0",
			"--admin_port=0",
			c.appDir,
		)
	default:
		err = fmt.Errorf("appenginetesting not supported on your platform of %s", runtime.GOOS)
		return err
	}

	c.child.Stdout = os.Stdout
	var stderr io.Reader
	stderr, err = c.child.StderrPipe()
	if err != nil {
		return err
	}

	if err = c.child.Start(); err != nil {
		return err
	}

	// Wait until we have read the URL of the API server.
	errc := make(chan error, 1)
	apic := make(chan string)
	adminc := make(chan string)
	modulec := make(chan string)
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			if c.debug == LogChild {
				log.Println(s.Text())
			}
			if match := apiServerAddrRE.FindSubmatch(s.Bytes()); match != nil {
				apic <- string(match[1])
			}
			if match := adminServerAddrRE.FindSubmatch(s.Bytes()); match != nil {
				adminc <- string(match[1])
			}
			if match := moduleServerAddrRE.FindSubmatch(s.Bytes()); match != nil {
				modulec <- string(match[1])
			}
		}
		if err = s.Err(); err != nil {
			errc <- err
		}
	}()

	for c.apiURL == "" || c.adminURL == "" || c.moduleURL == "" {
		select {
		case c.apiURL = <-apic:
		case c.adminURL = <-adminc:
		case c.moduleURL = <-modulec:
		case <-time.After(15 * time.Second):
			if p := c.child.Process; p != nil {
				p.Kill()
			}
			return errors.New("timeout starting child process")
		case err = <-errc:
			return fmt.Errorf("error reading child process stderr: %v", err)
		}
	}
	return nil
}

// NewContext returns a new AppEngine context with an empty datastore, etc.
// A nil Options is valid and means to use the default values.
func NewContext(opts *Options) (*Context, error) {
	req, _ := http.NewRequest("GET", "/", nil)
	c := &Context{
		appid:  opts.appId(),
		req:    req,
		queues: opts.taskQueues(),
		debug:  opts.debug(),
	}
	if err := c.startChild(); err != nil {
		return nil, err
	}
	return c, nil
}
