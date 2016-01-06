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
	"syscall"
	"time"

	"github.com/golang/protobuf/proto"

	"golang.org/x/net/context"
	"google.golang.org/appengine/user"
	"google.golang.org/appengine/internal"
	basepb "google.golang.org/appengine/internal/base"
)

// Trim out extraneous noise from logs
var logTrimRegexp = regexp.MustCompile(`  \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3}`)

// Statically verify that Context implements appengine.Context.
var _ context.Context = (*Context)(nil)

// httpClient is used to communicate with the helper child process's
// webserver.  We can't use http.DefaultClient anymore, as it's now
// blacklisted in App Engine 1.6.1 due to people misusing it in blog
// posts and such.  (but this is one of the rare valid uses of not
// using urlfetch)
var httpClient = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}}

// Dev app server script filename
const AppServerFileName = "dev_appserver.py"
const aeFakeName = "appenginetestingfake"

// Using -loglevel on the command line temporarily overrides the options in NewContext
var overrideLogLevel string

func init() {
	// TODO: Verify this works?
	// Check for override loglevel
	for i, a := range os.Args {
		if a == "-loglevel" {
			overrideLogLevel = os.Args[i+1]
		}
	}
}

// Context implements appengine.Context by running a dev_appserver.py
// process as a child and proxying all Context calls to the child.
// Use NewContext to create one.
type Context struct {
	appid      string
	req        *http.Request
	child      *exec.Cmd
	testingURL string   // URL of "stub" module to send requests to
	fakeAppDir string   // temp dir for application files
	queues     []string // list of queues to support
	debug      LogLevel // send the output of the application to console
	testing    TestingT
	wroteToLog bool           // used in TestLogging
	modules    []ModuleConfig // list of the modules that should start up on each test
}

type ModuleConfig struct {
	Name string // name of the module in the yaml file
	Path string // can be relative to the current working directory and should include the yaml file
}

func (c *Context) Deadline() (time.Time, bool) {
	return nil, false
}

func (c *Context) AppID() string {
	return c.appid
}

func (c *Context) logf(level LogLevel, format string, args ...interface{}) {
	if c.debug > level {
		return
	}
	s := fmt.Sprintf("%s\t%s\n", level, fmt.Sprintf(format, args...))
	s = logTrimRegexp.ReplaceAllLiteralString(s, " ")
	if c.testing == nil {
		log.Println(s)
	} else {
		c.testing.Logf(s)
	}
	c.wroteToLog = true // set if something was logged to support TestLogging unit test
}

type LogLevel int8

const (
	LogChild LogLevel = iota // LogChild logs all log levels plus what comes from the devappserver process
	LogDebug
	LogInfo
	LogWarning
	LogError
	LogCritical
)

func (ll LogLevel) String() string {
	switch ll {
	case LogChild:
		return "child"
	case LogDebug:
		return "debug"
	case LogInfo:
		return "info"
	case LogWarning:
		return "warning"
	case LogError:
		return "error"
	case LogCritical:
		return "critical"
	}
	return "unknown"
}

func (c *Context) Debugf(format string, args ...interface{}) {
	c.logf(LogDebug, format, args...)
}
func (c *Context) Infof(format string, args ...interface{}) {
	c.logf(LogInfo, format, args...)
}
func (c *Context) Warningf(format string, args ...interface{}) {
	c.logf(LogWarning, format, args...)
}
func (c *Context) Errorf(format string, args ...interface{}) {
	c.logf(LogError, format, args...)
}
func (c *Context) Criticalf(format string, args ...interface{}) {
	c.logf(LogCritical, format, args...)
}

func (c *Context) GetCurrentNamespace() string {
	return c.req.Header.Get("X-AppEngine-Current-Namespace")
}

func (c *Context) CurrentNamespace(namespace string) {
	c.req.Header.Set("X-AppEngine-Current-Namespace", namespace)
}

func (c *Context) CurrentUser() string {
	return c.req.Header.Get("X-AppEngine-Internal-User-Email")
}

func (c *Context) Login(u *user.User) {
	c.req.Header.Set("X-AppEngine-User-Email", u.Email)
	id := u.ID
	if id == "" {
		id = strconv.Itoa(int(crc32.Checksum([]byte(u.Email), crc32.IEEETable)))
	}
	c.req.Header.Set("X-AppEngine-User-Id", id)
	c.req.Header.Set("X-AppEngine-User-Federated-Identity", u.Email)
	c.req.Header.Set("X-AppEngine-User-Federated-Provider", u.FederatedProvider)
	if u.Admin {
		c.req.Header.Set("X-AppEngine-User-Is-Admin", "1")
	} else {
		c.req.Header.Set("X-AppEngine-User-Is-Admin", "0")
	}
}

func (c *Context) Logout() {
	c.req.Header.Del("X-AppEngine-User-Email")
	c.req.Header.Del("X-AppEngine-User-Id")
	c.req.Header.Del("X-AppEngine-User-Is-Admin")
	c.req.Header.Del("X-AppEngine-User-Federated-Identity")
	c.req.Header.Del("X-AppEngine-User-Federated-Provider")
}

func (c *Context) Call(service, method string, in, out internal.ProtoMessage, opts *internal.CallOptions) error {
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
		if mod, ok := internal.NamespaceMods[service]; ok {
			mod(in, cn)
		}
	}
	data, err := proto.Marshal(in)
	if err != nil {
		return err
	}
	req, _ := http.NewRequest("POST",
		fmt.Sprintf("%s/call?s=%s&m=%s", c.testingURL, service, method),
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
	defer func() {
		os.RemoveAll(c.fakeAppDir)
	}()
	if p := c.child.Process; p != nil {
		p.Signal(syscall.SIGTERM)
		if _, err := p.Wait(); err != nil {
			log.Fatalf("Error closing devappserver - %v", err)
		}
	}
	c.child = nil
}

// Options control optional behavior for NewContext.
type Options struct {
	// AppId to pretend to be. By default, "testapp"
	AppId      string // Required if using any Modules
	TaskQueues []string
	Debug      LogLevel
	Testing    TestingT
	Modules    []ModuleConfig
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

func (o *Options) modules() []ModuleConfig {
	if o == nil || len(o.Modules) == 0 {
		return []ModuleConfig{}
	}
	return o.Modules
}

func (o *Options) debug() LogLevel {
	if o == nil {
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

func (c *Context) startChild() error {
	python, err := findPython()
	if err != nil {
		return fmt.Errorf("Could not find python interpreter: %v", err)
	}

	c.fakeAppDir, err = ioutil.TempDir("", aeFakeName)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			// cleanup directory if there's an error in any of the steps following the creation of the child
			fmt.Printf("Cleaning up directory because of an error - %v\n", err)
			os.RemoveAll(c.fakeAppDir)
		}
	}()

	appBuf := new(bytes.Buffer)
	appTempl.Execute(appBuf, c.AppID())
	err = ioutil.WriteFile(filepath.Join(c.fakeAppDir, aeFakeName+".yaml"), appBuf.Bytes(), 0755)
	if err != nil {
		return err
	}

	c.modules = append(c.modules, ModuleConfig{Name: aeFakeName, Path: filepath.Join(c.fakeAppDir, aeFakeName+".yaml")})

	if len(c.queues) > 0 {
		var queueBuf bytes.Buffer
		queueTempl.Execute(&queueBuf, c.queues)
		err = ioutil.WriteFile(filepath.Join(c.fakeAppDir, "queue.yaml"), queueBuf.Bytes(), 0755)
		if err != nil {
			return fmt.Errorf("Error generating queue.yaml - %v", err)
		}
	}

	var helperBuf bytes.Buffer
	helperTempl.Execute(&helperBuf, aeFakeName)
	err = ioutil.WriteFile(filepath.Join(c.fakeAppDir, aeFakeName+".go"), helperBuf.Bytes(), 0644)
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

	startupComponents := []ComponentURL{
		ComponentURL{Name: "appenginetestingapi", Regex: regexp.MustCompile(`Starting API server at: (\S+)`)},
		ComponentURL{Name: "appenginetestingadmin", Regex: regexp.MustCompile(`Starting admin server at: (\S+)`)},
	}
	params := []string{}
	for _, val := range c.modules {
		startupComponents = append(startupComponents,
			ComponentURL{
				Name:  val.Name,
				Regex: regexp.MustCompile(fmt.Sprintf(`Starting module "%s" running at: (\S+)`, val.Name)),
			})
		params = append(params, val.Path)
	}

	switch runtime.GOOS {
	case "windows":
		c.child = exec.Command(
			"cmd",
			append([]string{"/C",
				python,
				devAppserver,
				"--clear_datastore=true",
				"--datastore_consistency_policy=consistent",
				"--skip_sdk_update_check=true",
				fmt.Sprintf("--storage_path=%s/data.datastore", c.fakeAppDir),
				fmt.Sprintf("--log_level=%s", appLog),
				"--dev_appserver_log_level=debug",
				"--port=0",
				"--api_port=0",
				"--admin_port=0",
			}, params...)...,
		)
	case "linux":
		fallthrough
	case "darwin":
		c.child = exec.Command(
			python,
			append([]string{devAppserver,
				"--clear_datastore=true",
				"--datastore_consistency_policy=consistent",
				"--skip_sdk_update_check=true",
				fmt.Sprintf("--storage_path=%s/data.datastore", c.fakeAppDir),
				fmt.Sprintf("--log_level=%s", appLog),
				"--dev_appserver_log_level=debug",
				"--port=0",
				"--api_port=0",
				"--admin_port=0",
			}, params...)...,
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

	// Wait until we have read the URL of all startup components
	errc := make(chan error, 1)
	componentsc := make(chan ComponentURL)
	startupComponentsCopy := make([]ComponentURL, len(startupComponents))
	copy(startupComponentsCopy, startupComponents)
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			if c.debug == LogChild {
				c.logf(LogChild, "%s", s.Text())
			}
			for _, componentURL := range startupComponentsCopy {
				if match := componentURL.Regex.FindSubmatch(s.Bytes()); match != nil {
					componentURL.URL = string(match[1])
					componentsc <- componentURL
				}
			}
		}
		if err := s.Err(); err != nil {
			errc <- err
		}
	}()

	for {
		allStarted := true
		for _, cu := range startupComponents {
			if cu.URL == "" {
				allStarted = false
				break
			}
		}
		if allStarted {
			return nil
		}
		select {
		case compURL := <-componentsc:
			if compURL.Name == aeFakeName {
				c.testingURL = compURL.URL
			}
			for x, value := range startupComponents {
				if value.Name == compURL.Name {
					startupComponents[x] = compURL
					break
				}
			}
		case <-time.After(15 * time.Second):
			if p := c.child.Process; p != nil {
				p.Kill()
			}
			c.Close()
			for _, value := range startupComponents {
				if value.URL == "" {
					for _, m := range c.modules {
						if m.Name == value.Name {
							return fmt.Errorf("timeout starting child process supporting - %s, does %s contain module config named %s?", m.Name, m.Path, m.Name)
						}
					}
					return fmt.Errorf("timeout starting child process supporting - %s", value.Name)
				}
			}
			return errors.New("Timeout starting process, this error is a bug in appenginetesting")
		case err = <-errc:
			c.Close()
			return fmt.Errorf("error reading child process stderr: %v", err)
		}
	}
}

type ComponentURL struct {
	Name  string
	Regex *regexp.Regexp
	URL   string
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

	switch overrideLogLevel {
	case "": // do nothing, no value set
	case "child":
		c.debug = LogChild
	case "debug":
		c.debug = LogDebug
	case "info":
		c.debug = LogInfo
	case "warning":
		c.debug = LogWarning
	case "error":
		c.debug = LogError
	case "critical":
		c.debug = LogCritical
	default:
		log.Fatalf("[appenginetesting] loglevel given %s, not a valid option, use one of child, debug, info, warning, error, or critical.", overrideLogLevel)
	}

	if opts != nil {
		c.testing = opts.Testing
	}
	c.modules = opts.modules()
	if (opts == nil || opts.AppId == "") && len(c.modules) > 0 {
		return nil, fmt.Errorf("Options.AppId required if using Modules")
	}

	for _, mod := range c.modules {
		if !fileExists(mod.Path) {
			return nil, fmt.Errorf("File %s not found for module %s!", mod.Path, mod.Name)
		}
	}

	if err := c.startChild(); err != nil {
		return nil, err
	}
	// in the hopes that the test program runs long, clean up non-closed Contexts
	runtime.SetFinalizer(c, func(deadContext *Context) {
		deadContext.Close()
	})
	return c, nil
}
