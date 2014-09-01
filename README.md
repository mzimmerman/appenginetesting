appenginetesting
===============

[![Build Status](https://travis-ci.org/mzimmerman/appenginetesting.svg?branch=master)](https://travis-ci.org/mzimmerman/appenginetesting)


**This package provides an automated way to test Go based appengine applications.  It differs from the appengine/aetest package as with appenginetesting the real application can be run alongside a stub module.***

E.g., Your application has the default module and two other modules (default, A, and B).  You can run your application under appenginetesting and perform tests against it.  Using the []ModuleConfig under Options tells what modules to start (default, A, and B) and appenginetesting additionally starts up a "mock" Context that can be used to generate and maniuplate data for testing.

History
------------
It's a combined fork of [gae-go-testing](https://github.com/tenntenn/gae-go-testing) and aetest with a number

Installation
------------
Before using this library, you have to install [appengine SDK](https://developers.google.com/appengine/downloads#Google_App_Engine_SDK_for_Go).

This library can be installed as following :

    $ go get -u github.com/mzimmerman/appenginetesting

Usage
-----

The [documentation](http://godoc.org/github.com/mzimmerman/appenginetesting) has some basic examples.  Also see the [example application](http://github.com/mzimmerman/appenginetesting/exampleapp).

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

For the details of various Options that can be used in NewContext, see http://godoc.org/github.com/mzimmerman/appenginetesting#Options
