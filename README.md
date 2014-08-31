appenginetesting
===============

[![Build Status](https://travis-ci.org/mzimmerman/appenginetesting.svg?branch=master)](https://travis-ci.org/mzimmerman/appenginetesting)

**The Go App Engine SDK now includes a testing package (appengine/aetest) that essentially replaces this package. You can find more information about it at [godoc](http://godoc.org/code.google.com/p/appengine-go/appengine/aetest).**

**This package is still being maintained since aetest lacks some of the features available in appenginetesting. (TaskQueues, Data Generation, Test Logging)**

It's a combined fork of [gae-go-testing](https://github.com/tenntenn/gae-go-testing) and aetest.

Installation
------------

Before using this library, you have to install
[appengine SDK](https://developers.google.com/appengine/downloads#Google_App_Engine_SDK_for_Go).

This library can be installed as following :

    $ go get -u github.com/mzimmerman/appenginetesting

Usage
-----

The
[documentation](http://godoc.org/github.com/mzimmerman/appenginetesting)
has some basic examples.  Also see the [example application](http://github.com/mzimmerman/appenginetesting/exampleapp).

```go
func TestMyApp(t *testing.T) {
        c, err := appenginetesting.NewContext(&appenginetesting.Options{
                Debug:   appenginetesting.LogDebug,
                Testing: t,
        })
        if err != nil {
                t.Fatalf("Could not get a context - %v", err)
        }
        defer c.Close()
        // do things
        c.Debugf("Log stuff")
}
```
goapp test
